#include <Arduino.h>
#include <WiFi.h>
#include <LittleFS.h>

#include "config.h"
#include "keyboard.h"
#include "storage.h"
#include "ui.h"

void setup() {
    Serial.begin(115200);

    // Apply timezone immediately so getLocalTime() is correct after deep sleep
    // wakeup (RTC keeps UTC time running; offset must be re-applied each boot).
    configTime(TZ_OFFSET_HOURS * 3600, 0, NTP_SERVER);

    // Power on peripherals (T-Deck has a power switch pin)
    pinMode(BOARD_POWERON, OUTPUT);
    digitalWrite(BOARD_POWERON, HIGH);
    delay(100);

    if (!LittleFS.begin(/*formatOnFail=*/true)) {
        Serial.println("LittleFS mount failed");
    }

    keyboardInit();
    trackballInit();
    uiInitHardware();

    g_storage.load();
    uiInitView();
}

void loop() {
    char key = keyboardRead();
    if (key == Key::NONE) key = trackballRead();
    if (key != Key::NONE) {
        uiHandleKey(key);
    }
    uiTick();
    uiDraw();
    delay(20);
}
