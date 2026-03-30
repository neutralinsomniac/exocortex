#pragma once
#include <Arduino.h>

enum class SyncResult {
    OK,
    ERR_WIFI,
    ERR_HTTP,
    ERR_CRYPTO,
    ERR_JSON,
};

// Performs a bidirectional sync against SYNC_URL using SYNC_TOKEN.
// Updates g_storage in place; caller should redraw UI afterwards.
// statusOut receives a human-readable status message.
SyncResult doSync(String& statusOut);
