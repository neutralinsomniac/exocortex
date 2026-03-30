#pragma once
#include <Arduino.h>

// Encrypt plaintext with AES-256-GCM, key = SHA-256(token).
// Output format: nonce(12) || ciphertext(n) || tag(16)
// Returns output length, or -1 on error.
// output must be at least plaintextLen + 28 bytes.
int encryptPayload(const String& token,
                   const uint8_t* plaintext, size_t plaintextLen,
                   uint8_t* output, size_t outputMaxLen);

// Decrypt a blob produced by encryptPayload (or the Go server).
// Returns plaintext length, or -1 on authentication/format failure.
// output must be at least dataLen - 28 bytes.
int decryptPayload(const String& token,
                   const uint8_t* data, size_t dataLen,
                   uint8_t* output, size_t outputMaxLen);
