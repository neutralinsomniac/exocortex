#pragma once
#include <Arduino.h>

// Special key codes (chosen not to overlap with printable ASCII)
namespace Key {
    static const char NONE      = 0x00;
    static const char ENTER     = 0x0D;
    static const char BACKSPACE = 0x08;
    static const char TAB       = 0x09;
    static const char ESCAPE    = 0x1B;
    // Trackball / navigation keys returned by the T-Deck keyboard firmware
    static const char UP        = 0xB5;
    static const char DOWN      = 0xB6;
    static const char LEFT      = 0xB4;
    static const char RIGHT     = 0xB7;
}

void keyboardInit();

// Returns the pressed key (Key::NONE if nothing pressed).
// Call from the main loop; polls the I2C keyboard controller.
char keyboardRead();

// Trackball: attach interrupts and init state.
void trackballInit();

// Returns a Key:: directional/enter code if the trackball has moved enough
// since the last call, or Key::NONE.  Rate-limited by TRACKBALL_REPEAT_MS.
char trackballRead();

// True if c is a printable character we can append to text.
inline bool isPrintable(char c) {
    return c >= 0x20 && c <= 0x7E;
}
