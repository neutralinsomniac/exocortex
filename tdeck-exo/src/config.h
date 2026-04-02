#pragma once

// ── WiFi ──────────────────────────────────────────────────────────────────────
#define WIFI_SSID     "your_ssid"
#define WIFI_PASSWORD "your_password"

// ── Exocortex server ──────────────────────────────────────────────────────────
// http://  → symmetric AES-256-GCM encryption (no TLS, payload encrypted)
// https:// → TLS (payload sent as plain JSON over encrypted connection)
#define SYNC_URL   "http://yourserver:8765"
#define SYNC_TOKEN "yoursecret"

// Skip TLS certificate verification (set false to verify with a CA cert)
#define TLS_INSECURE true

// ── Time ──────────────────────────────────────────────────────────────────────
#define NTP_SERVER "pool.ntp.org"
#define TZ_OFFSET_HOURS -5

// ── Optional static IP (leave undefined to use DHCP) ─────────────────────────
// #define STATIC_IP       "192.168.1.50"
// #define STATIC_GATEWAY  "192.168.1.1"
// #define STATIC_SUBNET   "255.255.255.0"
// #define STATIC_DNS      "1.1.1.1"

// ── T-Deck Plus pin definitions ───────────────────────────────────────────────
// These match the standard T-Deck; verify against your board's schematic.
#define BOARD_POWERON      10
#define BOARD_I2C_SDA      18
#define BOARD_I2C_SCL       8
#define BOARD_KB_I2C_ADDR  0x55

// SPI / display
#define BOARD_SPI_SCK      40
#define BOARD_SPI_MOSI     41
#define BOARD_SPI_MISO     38
#define BOARD_TFT_CS       12
#define BOARD_TFT_DC       11
#define BOARD_TFT_BL       42

// Battery ADC (voltage divider 1:2, so multiply raw voltage × 2)
#define BOARD_BAT_ADC      4

// Deep sleep after this many ms of inactivity (0 = disabled)
#define SLEEP_TIMEOUT_MS   30000

// ── Trackball pins ────────────────────────────────────────────────────────────
#define BOARD_TRACKBALL_UP     3
#define BOARD_TRACKBALL_DOWN  15
#define BOARD_TRACKBALL_LEFT   1
#define BOARD_TRACKBALL_RIGHT  2
#define BOARD_TRACKBALL_CLICK  0

// Minimum ms between repeated key events from the trackball
#define TRACKBALL_REPEAT_MS   120

// ── Display geometry ──────────────────────────────────────────────────────────
#define SCREEN_W  320
#define SCREEN_H  240

// Row height in pixels for lgfx::fonts::Font2 at textsize 1
#define CHAR_H    16
#define ROWS      (SCREEN_H / CHAR_H)   // 15

// ── Colours (RGB565) ──────────────────────────────────────────────────────────
#define COL_BG          0x0000   // black
#define COL_FG          0xFFFF   // white
#define COL_ACCENT      0x07FF   // cyan  (header/footer bar)
#define COL_CURSOR_BG   0x39C7   // dark blue highlight
#define COL_DONE_FG     0x8410   // grey  (done rows)
// Priority colours match the desktop TUI: 1=red, 2=yellow, 3=green, 4=white, 5=grey
#define COL_PRIO1_FG    0xF800   // red
#define COL_PRIO2_FG    0xFFE0   // yellow
#define COL_PRIO3_FG    0x07E0   // green
#define COL_PRIO4_FG    0xFFFF   // white (same as COL_FG)
#define COL_PRIO5_FG    0x8410   // grey  (same as COL_DONE_FG)
