#pragma once
#include <Arduino.h>

enum class UIState {
    BOOT,
    TAG_LIST,
    TAG_NEW,
    ROW_LIST,
    ROW_EDIT,
    SYNCING,
};

// Initialize display hardware and show boot screen.
void uiInitHardware();

// Set up the initial view after data has been loaded into g_storage.
void uiInitView();

void uiHandleKey(char key);
void uiDraw();
void uiTick();   // call every loop iteration to handle time-based redraws

// Called by main after a sync completes; triggers a redraw.
void uiSetStatus(const String& msg);
