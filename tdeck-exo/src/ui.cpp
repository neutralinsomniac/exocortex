#include "ui.h"
#include "storage.h"
#include "sync.h"
#include "keyboard.h"
#include "config.h"
#include <LovyanGFX.hpp>
#include <esp_sleep.h>
#include <vector>
#include <regex>

// ── LovyanGFX display setup ───────────────────────────────────────────────────

class LGFX : public lgfx::LGFX_Device {
    lgfx::Panel_ST7789 _panel;
    lgfx::Bus_SPI      _bus;
    lgfx::Light_PWM    _light;
public:
    LGFX() {
        {
            auto cfg      = _bus.config();
            cfg.spi_host  = SPI2_HOST;
            cfg.spi_mode  = 0;
            cfg.freq_write = 80000000;
            cfg.freq_read  = 20000000;
            cfg.spi_3wire  = false;
            cfg.use_lock   = true;
            cfg.dma_channel = SPI_DMA_CH_AUTO;
            cfg.pin_sclk   = BOARD_SPI_SCK;
            cfg.pin_mosi   = BOARD_SPI_MOSI;
            cfg.pin_miso   = BOARD_SPI_MISO;
            cfg.pin_dc     = BOARD_TFT_DC;
            _bus.config(cfg);
            _panel.setBus(&_bus);
        }
        {
            auto cfg          = _panel.config();
            cfg.pin_cs        = BOARD_TFT_CS;
            cfg.pin_rst       = -1;
            cfg.pin_busy      = -1;
            // ST7789 native GRAM is portrait: 240 wide × 320 tall.
            // setRotation(1) maps this to the 320×240 landscape we use.
            cfg.memory_width  = 240;
            cfg.memory_height = 320;
            cfg.panel_width   = 240;
            cfg.panel_height  = 320;
            cfg.offset_x      = 0;
            cfg.offset_y      = 0;
            cfg.offset_rotation = 0;
            cfg.invert        = true;
            cfg.rgb_order     = false;
            cfg.bus_shared    = true;
            _panel.config(cfg);
        }
        {
            auto cfg     = _light.config();
            cfg.pin_bl   = BOARD_TFT_BL;
            cfg.invert   = false;
            cfg.freq     = 44100;
            cfg.pwm_channel = 7;
            _light.config(cfg);
            _panel.setLight(&_light);
        }
        setPanel(&_panel);
    }
};

static LGFX              lcd;
static lgfx::LGFX_Sprite sprite(&lcd);   // double-buffer

// ── State ─────────────────────────────────────────────────────────────────────

static UIState  state          = UIState::BOOT;
static bool     dirty          = true;
static String   statusMsg;
static uint32_t lastActivityMs = 0;

// ── Background sync ───────────────────────────────────────────────────────────

static volatile bool syncRunning = false;
static volatile bool syncSuccess = false;

static void syncTask(void*) {
    String out;
    syncSuccess = (doSync(out) == SyncResult::OK);
    Serial.println(syncSuccess ? "Sync OK: " + out : "Sync ERR: " + out);
    syncRunning = false;
    vTaskDelete(nullptr);
}

// TAG_LIST state
static std::vector<int> filteredTagIdx;  // indices into g_storage.tags
static int      tagCursor   = 0;
static int      tagScroll   = 0;
static String   tagFilter;

// ROW_LIST state
static String           currentTagUUID;
static std::vector<int> visibleRows;     // indices into g_storage.rows
static int              rowCursor    = 0;
static int              rowScroll    = 0;
static bool             showDone     = false;

// ROW_EDIT / TAG_NEW state
static String editBuf;
static int    editCursor    = 0;   // insertion point within editBuf
static int    editRowIdx    = -1;  // -1 = new row
static int    editInsertPos = -1;  // target visible index for new-row insertion (-1 = append)

// Clipboard (cut/yank + paste)
static bool   hasClipboard = false;
static String clipText;
static int8_t clipPriority = 0;

// Battery (IP5306 at I2C 0x75, refreshed every minute)
static uint8_t batteryPct = 0;

static void refreshBattery() {
    // Battery voltage via ADC with 1:2 voltage divider.
    // analogReadMilliVolts uses ESP32 factory eFuse calibration for accuracy.
    analogSetPinAttenuation(BOARD_BAT_ADC, ADC_11db);  // 0–3.3 V range
    uint32_t mv = 0;
    for (int i = 0; i < 8; i++) mv += analogReadMilliVolts(BOARD_BAT_ADC);
    float voltage = (mv / 8.0f / 1000.0f) * 2.0f;  // undo 1:2 divider
    // LiPo: 3.0 V = 0%, 4.2 V = 100%
    float pct = (voltage - 3.0f) / 1.2f * 100.0f;
    if (pct < 0.0f)   pct = 0.0f;
    if (pct > 100.0f) pct = 100.0f;
    batteryPct = (uint8_t)pct;
}

// Navigation history (back stack of tag UUIDs)
static std::vector<String> tagHistory;

// ── Helpers ───────────────────────────────────────────────────────────────────

static void refreshFilteredTags() {
    filteredTagIdx.clear();
    for (int i = 0; i < (int)g_storage.tags.size(); i++) {
        if (tagFilter.isEmpty() ||
            g_storage.tags[i].name.indexOf(tagFilter) >= 0)
        {
            filteredTagIdx.push_back(i);
        }
    }
    if (tagCursor >= (int)filteredTagIdx.size())
        tagCursor = std::max(0, (int)filteredTagIdx.size() - 1);
}

static void refreshVisibleRows() {
    auto all = g_storage.rowsForTag(currentTagUUID);
    if (showDone) {
        visibleRows = all;
    } else {
        visibleRows.clear();
        for (int idx : all)
            if (!g_storage.rows[idx].done) visibleRows.push_back(idx);
    }
}

static void pruneCurrentTagIfEmpty() {
    if (currentTagUUID.isEmpty()) return;
    for (const Row& r : g_storage.rows)
        if (r.tagUUID == currentTagUUID) return;  // has rows, keep it
    int tagIdx = g_storage.findTagByUUID(currentTagUUID);
    if (tagIdx < 0) return;
    g_storage.deleteTag(tagIdx);
    g_storage.save();
    tagHistory.erase(std::remove(tagHistory.begin(), tagHistory.end(), currentTagUUID),
                     tagHistory.end());
    currentTagUUID = "";
}

static void openTag(const String& uuid) {
    pruneCurrentTagIfEmpty();
    if (!currentTagUUID.isEmpty() && currentTagUUID != uuid)
        tagHistory.push_back(currentTagUUID);
    currentTagUUID = uuid;
    refreshVisibleRows();
    rowCursor = 0;
    rowScroll = 0;
    if (g_storage.lastTagUUID != uuid) {
        g_storage.lastTagUUID = uuid;
        g_storage.save();
    }
    state = UIState::ROW_LIST;
    dirty = true;
}

static void openInbox() {
    int idx = g_storage.ensureTag("inbox");
    openTag(g_storage.tags[idx].uuid);
}

// Extract the first [[tag name]] reference from a row's text.
static String extractTagRef(const String& text) {
    int start = text.indexOf("[[");
    if (start < 0) return "";
    int end = text.indexOf("]]", start);
    if (end < 0) return "";
    return text.substring(start + 2, end);
}

static int contentRows() {
    // Number of list rows we can show between header and footer
    return ROWS - 2;
}

static void clampTagCursor() {
    if (tagCursor < 0) tagCursor = 0;
    if (tagCursor >= (int)filteredTagIdx.size()) tagCursor = std::max(0, (int)filteredTagIdx.size() - 1);
    if (tagCursor < tagScroll) tagScroll = tagCursor;
    if (tagCursor >= tagScroll + contentRows()) tagScroll = tagCursor - contentRows() + 1;
}

static void clampRowCursor() {
    if (rowCursor < 0) rowCursor = 0;
    if (rowCursor >= (int)visibleRows.size()) rowCursor = std::max(0, (int)visibleRows.size() - 1);
    if (rowCursor < rowScroll) rowScroll = rowCursor;
    if (rowCursor >= rowScroll + contentRows()) rowScroll = rowCursor - contentRows() + 1;
}

static void followRowCursor(int rowIdx) {
    for (int i = 0; i < (int)visibleRows.size(); i++) {
        if (visibleRows[i] == rowIdx) { rowCursor = i; break; }
    }
    clampRowCursor();
}

// ── Drawing ───────────────────────────────────────────────────────────────────

static void drawHeader(const String& left, const String& right = "") {
    sprite.fillRect(0, 0, SCREEN_W, CHAR_H, COL_ACCENT);
    sprite.setTextColor(COL_BG);
    sprite.setCursor(2, 0);
    sprite.print(left);
    if (!right.isEmpty()) {
        int rx = SCREEN_W - sprite.textWidth(right) - 2;
        sprite.setCursor(rx, 0);
        sprite.print(right);
    }
}

static void drawFooter(const String& hints) {
    int y = SCREEN_H - CHAR_H;
    sprite.fillRect(0, y, SCREEN_W, CHAR_H, COL_ACCENT);
    sprite.setTextColor(COL_BG);
    sprite.setCursor(2, y);
    sprite.print(hints);
}

static void drawRow(int screenRow, const String& text, bool cursor, bool done, int priority) {
    int y = CHAR_H + screenRow * CHAR_H;   // skip header

    uint16_t bg = cursor ? COL_CURSOR_BG : COL_BG;
    sprite.fillRect(0, y, SCREEN_W, CHAR_H, bg);

    sprite.setCursor(0, y);

    // Priority column: coloured number matching desktop TUI, dimmed dash if unset or done
    static const uint16_t priColors[] = { 0, COL_PRIO1_FG, COL_PRIO2_FG, COL_PRIO3_FG, COL_PRIO4_FG, COL_PRIO5_FG };
    if (!done && priority >= 1 && priority <= 5) {
        sprite.setTextColor(priColors[priority], bg);
        sprite.print(String(priority));
    } else {
        sprite.setTextColor(COL_DONE_FG, bg);
        sprite.print("-");
    }
    sprite.print(" ");

    sprite.setTextColor(done ? COL_DONE_FG : COL_FG, bg);
    sprite.print(text);
}

void uiDraw() {
    if (!dirty) return;
    dirty = false;

    sprite.fillScreen(COL_BG);
    sprite.setFont(&lgfx::fonts::Font2);
    sprite.setTextSize(1);

    switch (state) {
    // ── BOOT ────────────────────────────────────────────────────────────────
    case UIState::BOOT:
        // blank screen — boot is fast enough that no loading UI is needed
        break;

    // ── TAG_NEW ──────────────────────────────────────────────────────────────
    case UIState::TAG_NEW:
        drawHeader("New tag");
        drawFooter("Enter:create  Bsp:delete char  Esc:cancel");
        {
            int y = CHAR_H + CHAR_H;
            sprite.fillRect(0, CHAR_H, SCREEN_W, SCREEN_H - 2*CHAR_H, COL_BG);
            sprite.setTextColor(COL_FG, COL_BG);
            sprite.setCursor(2, y);
            sprite.print(editBuf + "_");
        }
        break;

    // ── TAG_LIST ─────────────────────────────────────────────────────────────
    case UIState::TAG_LIST: {
        String header = "TAGS";
        if (!tagFilter.isEmpty()) header += " /" + tagFilter;
        drawHeader(header, String(g_storage.tags.size()) + " total");
        drawFooter("Enter:open  Bsp:clear  type:search");

        int visible = contentRows();
        for (int i = 0; i < visible; i++) {
            int listIdx = tagScroll + i;
            if (listIdx >= (int)filteredTagIdx.size()) break;
            int tagIdx = filteredTagIdx[listIdx];
            const Tag& t = g_storage.tags[tagIdx];
            bool isCursor = (listIdx == tagCursor);

            int y = CHAR_H + i * CHAR_H;
            uint16_t bg = isCursor ? COL_CURSOR_BG : COL_BG;
            sprite.fillRect(0, y, SCREEN_W, CHAR_H, bg);
            sprite.setTextColor(isCursor ? COL_FG : COL_FG, bg);

            sprite.setCursor(2, y);
            sprite.print(t.name);
        }
        break;
    }

    // ── ROW_LIST ─────────────────────────────────────────────────────────────
    case UIState::ROW_LIST: {
        // Detect sync completion
        static bool wasSyncing = false;
        if (wasSyncing && !syncRunning) {
            statusMsg = syncSuccess ? "S" : "!";
            refreshVisibleRows();
            clampRowCursor();
        }
        wasSyncing = syncRunning;

        int tagIdx = g_storage.findTagByUUID(currentTagUUID);
        String tagName = tagIdx >= 0 ? g_storage.tags[tagIdx].name : "?";
        String hdrRight;
        if (syncRunning) {
            static const char* frames[] = {"/", "-", "\\", "|"};
            hdrRight = frames[(millis() / 250) % 4];
            dirty = true;  // keep animating
        } else {
            char buf[20];
            struct tm ti;
            if (getLocalTime(&ti, 0)) {
                snprintf(buf, sizeof(buf), "%02d:%02d %d%%%s%s",
                         ti.tm_hour, ti.tm_min, batteryPct,
                         statusMsg.isEmpty() ? "" : " ", statusMsg.c_str());
            } else {
                snprintf(buf, sizeof(buf), "%d%%%s%s",
                         batteryPct,
                         statusMsg.isEmpty() ? "" : " ", statusMsg.c_str());
            }
            hdrRight = buf;
        }
        drawHeader("[" + tagName + "]", hdrRight);
        drawFooter("o/O:new e:edit d:cut y:yank p/P:paste D:done s:sync");

        int visible = contentRows();
        for (int i = 0; i < visible; i++) {
            int listIdx = rowScroll + i;
            if (listIdx >= (int)visibleRows.size()) {
                int y = CHAR_H + i * CHAR_H;
                sprite.fillRect(0, y, SCREEN_W, CHAR_H, COL_BG);
                continue;
            }
            int rowIdx = visibleRows[listIdx];
            const Row& r = g_storage.rows[rowIdx];
            bool isCursor = (listIdx == rowCursor);
            drawRow(i, r.text, isCursor, r.done, r.priority);
        }
        break;
    }

    // ── ROW_EDIT ─────────────────────────────────────────────────────────────
    case UIState::ROW_EDIT: {
        int tagIdx = g_storage.findTagByUUID(currentTagUUID);
        String tagName = tagIdx >= 0 ? g_storage.tags[tagIdx].name : "?";
        drawHeader("Edit: [" + tagName + "]");
        drawFooter("Enter:save  Bsp:delete char  Esc:cancel");

        // Edit buffer with blinking cursor indicator
        int y = CHAR_H + CHAR_H;
        sprite.fillRect(0, CHAR_H, SCREEN_W, SCREEN_H - 2*CHAR_H, COL_BG);
        sprite.setTextColor(COL_FG, COL_BG);
        sprite.setCursor(2, y);

        // Render with word-wrap based on pixel position, not char count
        String display = editBuf.substring(0, editCursor) + "_" + editBuf.substring(editCursor);
        int row = 0;
        int xpos = 2;
        for (int i = 0; i < (int)display.length(); i++) {
            char c = display[i];
            int cw = sprite.textWidth(String(c));
            if (xpos + cw > SCREEN_W - 2) {
                row++;
                xpos = 2;
                sprite.setCursor(xpos, y + row * CHAR_H);
            }
            sprite.print(c);
            xpos += cw;
        }
        break;
    }
    }

    sprite.pushSprite(0, 0);
}

// ── Key handling ──────────────────────────────────────────────────────────────

static void goToSleep();  // defined below

void uiHandleKey(char key) {
    if (key == Key::NONE) return;
    lastActivityMs = millis();

    switch (state) {

    // ── TAG_NEW keys ─────────────────────────────────────────────────────────
    case UIState::TAG_NEW:
        if (key == Key::ENTER) {
            String name = editBuf;
            name.trim();
            if (!name.isEmpty()) {
                int idx = g_storage.ensureTag(name);
                g_storage.save();
                openTag(g_storage.tags[idx].uuid);
            } else {
                state = UIState::TAG_LIST;
                dirty = true;
            }
        } else if (key == Key::ESCAPE) {
            state = UIState::TAG_LIST;
            dirty = true;
        } else if (key == Key::BACKSPACE) {
            if (!editBuf.isEmpty()) { editBuf.remove(editBuf.length() - 1); dirty = true; }
        } else if (isPrintable(key)) {
            editBuf += key;
            dirty = true;
        }
        break;

    // ── TAG_LIST keys ────────────────────────────────────────────────────────
    case UIState::TAG_LIST:
        if (key == Key::ENTER) {
            if (!filteredTagIdx.empty()) {
                int idx = filteredTagIdx[tagCursor];
                openTag(g_storage.tags[idx].uuid);
            }
        } else if (key == Key::DOWN) {
            tagCursor++;
            clampTagCursor();
            dirty = true;
        } else if (key == Key::UP) {
            tagCursor--;
            clampTagCursor();
            dirty = true;
        } else if (key == 'i') {
            openInbox();
        } else if (key == Key::BACKSPACE) {
            if (!tagFilter.isEmpty()) {
                tagFilter.remove(tagFilter.length() - 1);
                tagCursor = 0;
                tagScroll = 0;
                refreshFilteredTags();
                dirty = true;
            } else if (!currentTagUUID.isEmpty()) {
                state = UIState::ROW_LIST;
                dirty = true;
            }
        } else if (isPrintable(key)) {
            tagFilter += key;
            tagCursor = 0;
            tagScroll = 0;
            refreshFilteredTags();
            dirty = true;
        }
        break;

    // ── ROW_LIST keys ────────────────────────────────────────────────────────
    case UIState::ROW_LIST:
        if (key == 'j' || key == Key::DOWN) {
            rowCursor++;
            clampRowCursor();
            dirty = true;
        } else if (key == 'k' || key == Key::UP) {
            rowCursor--;
            clampRowCursor();
            dirty = true;
        } else if (key == Key::ENTER || key == 'l') {
            // Follow [[tag]] link in selected row
            if (!visibleRows.empty() && rowCursor < (int)visibleRows.size()) {
                const Row& r = g_storage.rows[visibleRows[rowCursor]];
                String ref = extractTagRef(r.text);
                if (!ref.isEmpty()) {
                    int idx = g_storage.ensureTag(ref);
                    openTag(g_storage.tags[idx].uuid);
                }
            }
        } else if (key == 'n') {
            // New tag
            editBuf = "";
            state   = UIState::TAG_NEW;
            dirty   = true;
        } else if (key == 'o') {
            // New row below cursor
            editBuf       = "";
            editCursor    = 0;
            editRowIdx    = -1;
            editInsertPos = visibleRows.empty() ? 0 : rowCursor + 1;
            state         = UIState::ROW_EDIT;
            dirty         = true;
        } else if (key == 'O') {
            // New row above cursor
            editBuf       = "";
            editCursor    = 0;
            editRowIdx    = -1;
            editInsertPos = visibleRows.empty() ? 0 : rowCursor;
            state         = UIState::ROW_EDIT;
            dirty         = true;
        } else if (key == 'e') {
            // Edit selected row
            if (!visibleRows.empty() && rowCursor < (int)visibleRows.size()) {
                editRowIdx = visibleRows[rowCursor];
                editBuf    = g_storage.rows[editRowIdx].text;
                editCursor = editBuf.length();
                state      = UIState::ROW_EDIT;
                dirty      = true;
            }
        } else if (key == 'd') {
            // Cut: copy to clipboard then delete
            if (!visibleRows.empty() && rowCursor < (int)visibleRows.size()) {
                const Row& r = g_storage.rows[visibleRows[rowCursor]];
                clipText      = r.text;
                clipPriority  = r.priority;
                hasClipboard  = true;
                g_storage.deleteRow(visibleRows[rowCursor]);
                g_storage.save();
                refreshVisibleRows();
                clampRowCursor();
                dirty = true;
            }
        } else if (key == 'y') {
            // Yank: copy to clipboard without deleting
            if (!visibleRows.empty() && rowCursor < (int)visibleRows.size()) {
                const Row& r = g_storage.rows[visibleRows[rowCursor]];
                clipText      = r.text;
                clipPriority  = r.priority;
                hasClipboard  = true;
                statusMsg     = "Y";
                dirty         = true;
            }
        } else if (key == 'p' || key == 'P') {
            // Paste clipboard row after cursor (p) or before cursor (P)
            if (hasClipboard) {
                int target = visibleRows.empty() ? 0
                           : (key == 'p' ? rowCursor + 1 : rowCursor);

                g_storage.addRow(currentTagUUID, clipText);
                int newIdx = (int)g_storage.rows.size() - 1;
                if (clipPriority > 0)
                    g_storage.updateRowPriority(newIdx, clipPriority);

                refreshVisibleRows();

                // Find new row's position and swap it up toward target,
                // stopping at priority/done group boundaries.
                int cur = (int)visibleRows.size() - 1;
                for (int i = 0; i < (int)visibleRows.size(); i++)
                    if (visibleRows[i] == newIdx) { cur = i; break; }

                while (cur > target) {
                    const Row& a = g_storage.rows[visibleRows[cur]];
                    const Row& b = g_storage.rows[visibleRows[cur - 1]];
                    if (a.done != b.done || a.priority != b.priority) break;
                    g_storage.swapRowRanks(visibleRows[cur], visibleRows[cur - 1]);
                    std::swap(visibleRows[cur], visibleRows[cur - 1]);
                    cur--;
                }

                g_storage.save();
                refreshVisibleRows();
                followRowCursor(newIdx);
                dirty = true;
            }
        } else if (key == 'D') {
            // Toggle done
            if (!visibleRows.empty() && rowCursor < (int)visibleRows.size()) {
                int rowIdx = visibleRows[rowCursor];
                g_storage.toggleDone(rowIdx);
                g_storage.save();
                refreshVisibleRows();
                followRowCursor(rowIdx);
                dirty = true;
            }
        } else if (key == 'J') {
            // Move row down within its priority+done group
            if (!visibleRows.empty() && rowCursor < (int)visibleRows.size() - 1) {
                const Row& cur  = g_storage.rows[visibleRows[rowCursor]];
                const Row& next = g_storage.rows[visibleRows[rowCursor + 1]];
                if (cur.done == next.done && cur.priority == next.priority) {
                    g_storage.swapRowRanks(visibleRows[rowCursor], visibleRows[rowCursor + 1]);
                    g_storage.save();
                    refreshVisibleRows();
                    rowCursor++;
                    clampRowCursor();
                    dirty = true;
                }
            }
        } else if (key == 'K') {
            // Move row up within its priority+done group
            if (!visibleRows.empty() && rowCursor > 0) {
                const Row& cur  = g_storage.rows[visibleRows[rowCursor]];
                const Row& prev = g_storage.rows[visibleRows[rowCursor - 1]];
                if (cur.done == prev.done && cur.priority == prev.priority) {
                    g_storage.swapRowRanks(visibleRows[rowCursor], visibleRows[rowCursor - 1]);
                    g_storage.save();
                    refreshVisibleRows();
                    rowCursor--;
                    clampRowCursor();
                    dirty = true;
                }
            }
        } else if (key == 'h') {
            // Show/hide done rows
            showDone = !showDone;
            refreshVisibleRows();
            clampRowCursor();
            dirty = true;
        } else if (key >= '1' && key <= '5') {
            // Set priority 1-5; press same key again to clear
            if (!visibleRows.empty() && rowCursor < (int)visibleRows.size()) {
                int8_t prio = (int8_t)(key - '0');
                int rowIdx  = visibleRows[rowCursor];
                if (g_storage.rows[rowIdx].priority == prio) prio = 0;
                g_storage.updateRowPriority(rowIdx, prio);
                g_storage.save();
                refreshVisibleRows();
                followRowCursor(rowIdx);
                dirty = true;
            }
        } else if (key == '0') {
            // Clear priority
            if (!visibleRows.empty() && rowCursor < (int)visibleRows.size()) {
                int rowIdx = visibleRows[rowCursor];
                g_storage.updateRowPriority(rowIdx, 0);
                g_storage.save();
                refreshVisibleRows();
                followRowCursor(rowIdx);
                dirty = true;
            }
        } else if (key == 's') {
            // Sync (runs on core 0; animation drives from uiDraw)
            if (!syncRunning) {
                syncRunning = true;
                statusMsg   = "";
                xTaskCreatePinnedToCore(syncTask, "sync", 8192, nullptr, 1, nullptr, 0);
                dirty = true;
            }
        } else if (key == 't') {
            // Go to tag list
            pruneCurrentTagIfEmpty();
            tagFilter = "";
            refreshFilteredTags();
            state = UIState::TAG_LIST;
            dirty = true;
        } else if (key == Key::BACKSPACE) {
            goToSleep();
        } else if (key == 'i') {
            openInbox();
        } else if (key == 'b') {
            // Back in history
            if (!tagHistory.empty()) {
                pruneCurrentTagIfEmpty();
                String prev = tagHistory.back();
                tagHistory.pop_back();
                currentTagUUID = prev;
                refreshVisibleRows();
                rowCursor = 0;
                rowScroll = 0;
                dirty     = true;
            }
        }
        break;

    // ── ROW_EDIT keys ────────────────────────────────────────────────────────
    case UIState::ROW_EDIT:
        if (key == Key::ENTER) {
            String text = editBuf;
            text.trim();
            if (!text.isEmpty()) {
                if (editRowIdx < 0) {
                    g_storage.addRow(currentTagUUID, text);
                    int newIdx = (int)g_storage.rows.size() - 1;
                    refreshVisibleRows();
                    if (editInsertPos >= 0) {
                        int target = std::min(editInsertPos, (int)visibleRows.size() - 1);
                        int cur    = (int)visibleRows.size() - 1;
                        for (int i = 0; i < (int)visibleRows.size(); i++)
                            if (visibleRows[i] == newIdx) { cur = i; break; }
                        while (cur > target) {
                            const Row& a = g_storage.rows[visibleRows[cur]];
                            const Row& b = g_storage.rows[visibleRows[cur - 1]];
                            if (a.done != b.done || a.priority != b.priority) break;
                            g_storage.swapRowRanks(visibleRows[cur], visibleRows[cur - 1]);
                            std::swap(visibleRows[cur], visibleRows[cur - 1]);
                            cur--;
                        }
                        refreshVisibleRows();
                    }
                    g_storage.save();
                    followRowCursor(newIdx);
                } else {
                    g_storage.updateRowText(editRowIdx, text);
                    g_storage.save();
                    refreshVisibleRows();
                    followRowCursor(editRowIdx);
                }
            }
            state = UIState::ROW_LIST;
            dirty = true;
        } else if (key == Key::ESCAPE) {
            state = UIState::ROW_LIST;
            dirty = true;
        } else if (key == Key::LEFT) {
            if (editCursor > 0) { editCursor--; dirty = true; }
        } else if (key == Key::RIGHT) {
            if (editCursor < (int)editBuf.length()) { editCursor++; dirty = true; }
        } else if (key == Key::BACKSPACE) {
            if (editCursor > 0) {
                editBuf.remove(editCursor - 1, 1);
                editCursor--;
                dirty = true;
            }
        } else if (isPrintable(key)) {
            editBuf = editBuf.substring(0, editCursor) + key + editBuf.substring(editCursor);
            editCursor++;
            dirty = true;
        }
        break;

    default:
        break;
    }
}

void uiSetStatus(const String& msg) {
    statusMsg = msg;
    dirty     = true;
}

static void goToSleep() {
    lcd.setBrightness(0);
    sprite.fillScreen(COL_BG);
    sprite.pushSprite(0, 0);
    digitalWrite(BOARD_POWERON, LOW);

    esp_sleep_enable_ext1_wakeup(1ULL << BOARD_TRACKBALL_CLICK, ESP_EXT1_WAKEUP_ANY_LOW);
    esp_deep_sleep_start();
}

void uiTick() {
#if SLEEP_TIMEOUT_MS > 0
    if (!syncRunning && millis() - lastActivityMs > SLEEP_TIMEOUT_MS)
        goToSleep();
#endif

    if (state != UIState::ROW_LIST || syncRunning) return;
    static uint32_t lastMin = UINT32_MAX;
    struct tm ti;
    if (!getLocalTime(&ti, 0)) return;
    uint32_t curMin = (uint32_t)ti.tm_hour * 60 + ti.tm_min;
    if (curMin != lastMin) {
        lastMin = curMin;
        refreshBattery();
        dirty   = true;
    }
}

// ── Init ─────────────────────────────────────────────────────────────────────

void uiInitHardware() {
    lcd.init();
    lcd.setRotation(1);
    lcd.setBrightness(200);

    sprite.createSprite(SCREEN_W, SCREEN_H);
    sprite.setFont(&lgfx::fonts::Font2);
    sprite.setTextSize(1);

    state = UIState::BOOT;
    dirty = true;
    uiDraw();
}

void uiInitView() {
    statusMsg = "";
    refreshBattery();
    // Seed tag list from now-loaded g_storage
    refreshFilteredTags();

    // Reopen last tag or go to inbox
    if (!g_storage.lastTagUUID.isEmpty() &&
        g_storage.findTagByUUID(g_storage.lastTagUUID) >= 0)
    {
        openTag(g_storage.lastTagUUID);
    } else {
        openInbox();
    }
}
