#include "keyboard.h"
#include "config.h"
#include <Wire.h>

void keyboardInit() {
    Wire.begin(BOARD_I2C_SDA, BOARD_I2C_SCL);
    Wire.setClock(400000);
}

char keyboardRead() {
    Wire.requestFrom((uint8_t)BOARD_KB_I2C_ADDR, (uint8_t)1);
    if (!Wire.available()) return Key::NONE;
    char c = (char)Wire.read();
    return c;  // 0x00 means no key pressed
}

// ── Trackball ─────────────────────────────────────────────────────────────────

static volatile int16_t tb_up    = 0;
static volatile int16_t tb_down  = 0;
static volatile int16_t tb_left  = 0;
static volatile int16_t tb_right = 0;
static volatile int16_t tb_click = 0;

static void IRAM_ATTR isrUp()    { tb_up++;    }
static void IRAM_ATTR isrDown()  { tb_down++;  }
static void IRAM_ATTR isrLeft()  { tb_left++;  }
static void IRAM_ATTR isrRight() { tb_right++; }
static void IRAM_ATTR isrClick() { tb_click++; }

void trackballInit() {
    const uint8_t pins[] = {
        BOARD_TRACKBALL_UP, BOARD_TRACKBALL_DOWN,
        BOARD_TRACKBALL_LEFT, BOARD_TRACKBALL_RIGHT,
        BOARD_TRACKBALL_CLICK
    };
    for (uint8_t p : pins) {
        pinMode(p, INPUT_PULLUP);
    }
    attachInterrupt(digitalPinToInterrupt(BOARD_TRACKBALL_UP),    isrUp,    FALLING);
    attachInterrupt(digitalPinToInterrupt(BOARD_TRACKBALL_DOWN),  isrDown,  FALLING);
    attachInterrupt(digitalPinToInterrupt(BOARD_TRACKBALL_LEFT),  isrLeft,  FALLING);
    attachInterrupt(digitalPinToInterrupt(BOARD_TRACKBALL_RIGHT), isrRight, FALLING);
    attachInterrupt(digitalPinToInterrupt(BOARD_TRACKBALL_CLICK), isrClick, FALLING);
}

char trackballRead() {
    static uint32_t lastEmitMs = 0;
    uint32_t now = millis();
    if (now - lastEmitMs < TRACKBALL_REPEAT_MS) return Key::NONE;

    // Snapshot and clear counters atomically enough for single-core polling
    noInterrupts();
    int16_t u = tb_up,  d = tb_down,
            l = tb_left, r = tb_right, c = tb_click;
    tb_up = tb_down = tb_left = tb_right = tb_click = 0;
    interrupts();

    char key = Key::NONE;
    // Pick the dominant axis; click takes priority
    if (c > 0) {
        key = Key::ENTER;
    } else if (u > 0 || d > 0 || l > 0 || r > 0) {
        int16_t best = 0;
        if (u > best) { best = u; key = Key::UP;    }
        if (d > best) { best = d; key = Key::DOWN;  }
        if (l > best) { best = l; key = Key::LEFT;  }
        if (r > best) { best = r; key = Key::RIGHT; }
    }

    if (key != Key::NONE) lastEmitMs = now;
    return key;
}
