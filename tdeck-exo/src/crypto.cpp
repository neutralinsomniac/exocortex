#include "crypto.h"
#include <mbedtls/gcm.h>
#include <mbedtls/sha256.h>
#include <esp_random.h>

static const size_t NONCE_SIZE = 12;
static const size_t TAG_SIZE   = 16;

static void deriveKey(const String& token, uint8_t key[32]) {
    mbedtls_sha256(
        reinterpret_cast<const unsigned char*>(token.c_str()),
        token.length(), key, /*is224=*/0);
}

int encryptPayload(const String& token,
                   const uint8_t* plaintext, size_t plaintextLen,
                   uint8_t* output, size_t outputMaxLen)
{
    if (outputMaxLen < NONCE_SIZE + plaintextLen + TAG_SIZE) return -1;

    uint8_t key[32];
    deriveKey(token, key);

    uint8_t* nonce      = output;
    uint8_t* ciphertext = output + NONCE_SIZE;
    uint8_t  tag[TAG_SIZE];

    esp_fill_random(nonce, NONCE_SIZE);

    mbedtls_gcm_context gcm;
    mbedtls_gcm_init(&gcm);
    if (mbedtls_gcm_setkey(&gcm, MBEDTLS_CIPHER_ID_AES, key, 256) != 0) {
        mbedtls_gcm_free(&gcm);
        return -1;
    }

    int ret = mbedtls_gcm_crypt_and_tag(
        &gcm, MBEDTLS_GCM_ENCRYPT,
        plaintextLen,
        nonce, NONCE_SIZE,
        /*aad=*/nullptr, 0,
        plaintext, ciphertext,
        TAG_SIZE, tag);
    mbedtls_gcm_free(&gcm);
    if (ret != 0) return -1;

    // Append GCM tag after ciphertext (matches Go's gcm.Seal layout)
    memcpy(ciphertext + plaintextLen, tag, TAG_SIZE);
    return NONCE_SIZE + plaintextLen + TAG_SIZE;
}

int decryptPayload(const String& token,
                   const uint8_t* data, size_t dataLen,
                   uint8_t* output, size_t outputMaxLen)
{
    if (dataLen < NONCE_SIZE + TAG_SIZE) return -1;
    size_t ciphertextLen = dataLen - NONCE_SIZE - TAG_SIZE;
    if (outputMaxLen < ciphertextLen) return -1;

    uint8_t key[32];
    deriveKey(token, key);

    const uint8_t* nonce      = data;
    const uint8_t* ciphertext = data + NONCE_SIZE;
    const uint8_t* tag        = data + NONCE_SIZE + ciphertextLen;

    mbedtls_gcm_context gcm;
    mbedtls_gcm_init(&gcm);
    if (mbedtls_gcm_setkey(&gcm, MBEDTLS_CIPHER_ID_AES, key, 256) != 0) {
        mbedtls_gcm_free(&gcm);
        return -1;
    }

    int ret = mbedtls_gcm_auth_decrypt(
        &gcm, ciphertextLen,
        nonce, NONCE_SIZE,
        /*aad=*/nullptr, 0,
        tag, TAG_SIZE,
        ciphertext, output);
    mbedtls_gcm_free(&gcm);
    if (ret != 0) return -1;
    return (int)ciphertextLen;
}
