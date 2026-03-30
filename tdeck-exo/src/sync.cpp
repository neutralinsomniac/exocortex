#include "sync.h"
#include "storage.h"
#include "crypto.h"
#include "config.h"
#include <ArduinoJson.h>
#include <HTTPClient.h>
#include <WiFiClientSecure.h>
#include <WiFi.h>
#include <time.h>
#include <algorithm>

static bool ensureWiFi(String& errOut) {
    if (WiFi.status() == WL_CONNECTED) return true;

    WiFi.mode(WIFI_STA);
#ifdef STATIC_IP
    {
        IPAddress ip, gw, sn, dns;
        ip.fromString(STATIC_IP);
        gw.fromString(STATIC_GATEWAY);
        sn.fromString(STATIC_SUBNET);
        dns.fromString(STATIC_DNS);
        WiFi.config(ip, gw, sn, dns);
    }
#endif
    WiFi.begin(WIFI_SSID, WIFI_PASSWORD);
    int attempts = 0;
    while (WiFi.status() != WL_CONNECTED && attempts < 20) {
        delay(500);
        attempts++;
    }
    if (WiFi.status() != WL_CONNECTED) {
        errOut = "WiFi connect failed";
        return false;
    }

    // Sync time on first connection so timestamps are correct
    configTime(TZ_OFFSET_HOURS * 3600, 0, NTP_SERVER);
    struct tm ti;
    int tries = 0;
    while (!getLocalTime(&ti, 1000) && tries < 10) tries++;

    // Re-timestamp any rows/tags modified before the clock was set.
    // Pre-NTP the ESP32 RTC starts at epoch 0, so anything before 2020 is bogus.
    // We bump those to now so the server's LWW accepts our local changes.
    static const int64_t MIN_VALID_TS = 1577836800LL * 1000000000LL; // 2020-01-01
    struct timeval tv;
    gettimeofday(&tv, nullptr);
    int64_t now = (int64_t)tv.tv_sec * 1000000000LL + (int64_t)tv.tv_usec * 1000LL;
    bool dirty = false;
    for (Row& r : g_storage.rows) {
        if (r.updatedTS > 0 && r.updatedTS < MIN_VALID_TS) {
            r.updatedTS = now++;
            dirty = true;
        }
    }
    for (Tag& t : g_storage.tags) {
        if (t.updatedTS > 0 && t.updatedTS < MIN_VALID_TS) {
            t.updatedTS = now++;
            dirty = true;
        }
    }
    for (Tombstone& ts : g_storage.deletedRows) {
        if (ts.deletedTS > 0 && ts.deletedTS < MIN_VALID_TS) {
            ts.deletedTS = now++;
            dirty = true;
        }
    }
    for (Tombstone& ts : g_storage.deletedTags) {
        if (ts.deletedTS > 0 && ts.deletedTS < MIN_VALID_TS) {
            ts.deletedTS = now++;
            dirty = true;
        }
    }
    if (dirty) g_storage.save();

    return true;
}

// ── JSON encode ───────────────────────────────────────────────────────────────

static String buildRequestJSON(int64_t since,
                                const std::vector<Tag>&       tags,
                                const std::vector<Row>&       rows,
                                const std::vector<Tombstone>& deletedRows,
                                const std::vector<Tombstone>& deletedTags)
{
    JsonDocument doc;
    doc["since"] = since;

    // Tags: include server-side integer ID so the remote can map.
    // We use the tag's vector index + 1 as a stable local "server ID".
    // Field names must match Go struct field names exactly (no json tags on Tag/Row).
    JsonArray jTags = doc["tags"].to<JsonArray>();
    for (int i = 0; i < (int)tags.size(); i++) {
        JsonObject o = jTags.add<JsonObject>();
        o["ID"]        = i + 1;   // local index + 1 as stable ID within this payload
        o["Name"]      = tags[i].name;
        o["UpdatedTS"] = tags[i].updatedTS;
        o["UUID"]      = tags[i].uuid;
    }

    JsonArray jRows = doc["rows"].to<JsonArray>();
    for (const Row& r : rows) {
        int tagIdx = g_storage.findTagByUUID(r.tagUUID);
        if (tagIdx < 0) continue;
        JsonObject o = jRows.add<JsonObject>();
        o["ID"]          = 0;
        o["TagID"]       = tagIdx + 1;
        o["Rank"]        = r.rank;
        o["Text"]        = r.text;
        o["ParentRowID"] = 0;
        o["UpdatedTS"]   = r.updatedTS;
        o["Note"]        = r.note;
        o["Priority"]    = r.priority;
        o["Done"]        = r.done;
        o["UUID"]        = r.uuid;
    }

    JsonArray jDR = doc["deleted_rows"].to<JsonArray>();
    for (const Tombstone& ts : deletedRows) {
        JsonObject o = jDR.add<JsonObject>();
        o["key"]        = ts.key;
        o["deleted_ts"] = ts.deletedTS;
    }

    JsonArray jDT = doc["deleted_tags"].to<JsonArray>();
    for (const Tombstone& ts : deletedTags) {
        JsonObject o = jDT.add<JsonObject>();
        o["key"]        = ts.key;
        o["deleted_ts"] = ts.deletedTS;
    }

    String out;
    serializeJson(doc, out);
    return out;
}

// ── JSON decode ───────────────────────────────────────────────────────────────

static bool parseResponse(const uint8_t* data, size_t len,
                           std::vector<ExoStorage::RemoteTag>&  outTags,
                           std::vector<Row>&                     outRows,
                           std::vector<Tombstone>&               outDeletedRows,
                           std::vector<Tombstone>&               outDeletedTags,
                           int64_t&                              outServerTS)
{
    JsonDocument doc;
    DeserializationError err = deserializeJson(doc, data, len);
    if (err) return false;

    outServerTS = doc["server_ts"] | (int64_t)0;

    // Build serverID → Tag map for resolving row tag_ids
    outTags.clear();
    for (JsonObject o : doc["tags"].as<JsonArray>()) {
        ExoStorage::RemoteTag rt;
        rt.serverID      = o["ID"]        | (int64_t)0;
        rt.tag.uuid      = o["UUID"]      | "";
        rt.tag.name      = o["Name"]      | "";
        rt.tag.updatedTS = o["UpdatedTS"] | (int64_t)0;
        outTags.push_back(std::move(rt));
    }

    outRows.clear();
    for (JsonObject o : doc["rows"].as<JsonArray>()) {
        Row r;
        r.uuid      = o["UUID"]      | "";
        // Temporarily store the server's integer TagID as a decimal string
        // in tagUUID; applyRemote() converts it back via atoll.
        r.tagUUID   = String((int64_t)(o["TagID"] | (int64_t)0));
        r.text      = o["Text"]      | "";
        r.note      = o["Note"]      | "";
        r.rank      = o["Rank"]      | 0;
        r.updatedTS = o["UpdatedTS"] | (int64_t)0;
        r.priority  = o["Priority"]  | 0;
        r.done      = o["Done"]      | false;
        outRows.push_back(std::move(r));
    }

    outDeletedRows.clear();
    for (JsonObject o : doc["deleted_rows"].as<JsonArray>()) {
        Tombstone ts;
        ts.key       = o["key"]        | "";
        ts.deletedTS = o["deleted_ts"] | (int64_t)0;
        outDeletedRows.push_back(std::move(ts));
    }

    outDeletedTags.clear();
    for (JsonObject o : doc["deleted_tags"].as<JsonArray>()) {
        Tombstone ts;
        ts.key       = o["key"]        | "";
        ts.deletedTS = o["deleted_ts"] | (int64_t)0;
        outDeletedTags.push_back(std::move(ts));
    }

    return true;
}

// ── HTTP POST ─────────────────────────────────────────────────────────────────

SyncResult doSync(String& statusOut) {
    if (!ensureWiFi(statusOut)) return SyncResult::ERR_WIFI;

    const String url   = String(SYNC_URL);
    const String token = String(SYNC_TOKEN);
    const bool encrypt = url.startsWith("http://");

    // Build local payload
    std::vector<Tag>       localTags;
    std::vector<Row>       localRows;
    std::vector<Tombstone> localDeletedRows;
    std::vector<Tombstone> localDeletedTags;
    g_storage.buildPayload(g_storage.lastPushTS,
                           localTags, localRows,
                           localDeletedRows, localDeletedTags);

    String reqJSON = buildRequestJSON(g_storage.lastSyncTS,
                                      localTags, localRows,
                                      localDeletedRows, localDeletedTags);

    // Prepare body (plain JSON or encrypted blob)
    size_t   bodyLen  = reqJSON.length();
    uint8_t* bodyBuf  = nullptr;
    String   bodyMIME = "application/json";

    if (encrypt) {
        size_t maxEncLen = bodyLen + 28;
        bodyBuf = (uint8_t*)malloc(maxEncLen);
        if (!bodyBuf) { statusOut = "OOM"; return SyncResult::ERR_CRYPTO; }
        int encLen = encryptPayload(token,
                                    (const uint8_t*)reqJSON.c_str(), bodyLen,
                                    bodyBuf, maxEncLen);
        if (encLen < 0) {
            free(bodyBuf);
            statusOut = "Encryption failed";
            return SyncResult::ERR_CRYPTO;
        }
        bodyLen  = encLen;
        bodyMIME = "application/octet-stream";
    }

    // Open HTTP connection
    HTTPClient http;
    WiFiClientSecure secureClient;
    if (url.startsWith("https://")) {
        if (TLS_INSECURE) secureClient.setInsecure();
        http.begin(secureClient, url + "/sync");
    } else {
        http.begin(url + "/sync");
    }
    http.addHeader("Content-Type", bodyMIME);
    if (!encrypt && token.length() > 0) {
        http.addHeader("Authorization", "Bearer " + token);
    }
    http.setTimeout(30000);

    const uint8_t* postData = encrypt
        ? bodyBuf
        : (const uint8_t*)reqJSON.c_str();

    int httpCode = http.POST(const_cast<uint8_t*>(postData), bodyLen);
    if (bodyBuf) { free(bodyBuf); bodyBuf = nullptr; }

    if (httpCode != 200) {
        statusOut = "HTTP " + String(httpCode) + ": " + http.getString();
        http.end();
        return SyncResult::ERR_HTTP;
    }

    // Read response body
    int respSize = http.getSize();
    uint8_t* respBuf = nullptr;
    size_t respLen = 0;

    if (respSize > 0) {
        respBuf = (uint8_t*)malloc(respSize);
        if (!respBuf) { http.end(); statusOut = "OOM"; return SyncResult::ERR_JSON; }
        WiFiClient* stream = http.getStreamPtr();
        respLen = stream->readBytes(respBuf, respSize);
    } else {
        // chunked transfer: read until closed
        String body = http.getString();
        respLen = body.length();
        respBuf = (uint8_t*)malloc(respLen + 1);
        if (!respBuf) { http.end(); statusOut = "OOM"; return SyncResult::ERR_JSON; }
        memcpy(respBuf, body.c_str(), respLen);
    }
    http.end();

    // Decrypt if needed
    uint8_t* jsonBuf = respBuf;
    size_t   jsonLen = respLen;
    uint8_t* decBuf  = nullptr;

    if (encrypt) {
        decBuf = (uint8_t*)malloc(respLen);
        if (!decBuf) { free(respBuf); statusOut = "OOM"; return SyncResult::ERR_CRYPTO; }
        int decLen = decryptPayload(token, respBuf, respLen, decBuf, respLen);
        free(respBuf); respBuf = nullptr;
        if (decLen < 0) {
            free(decBuf);
            statusOut = "Decryption failed";
            return SyncResult::ERR_CRYPTO;
        }
        jsonBuf = decBuf;
        jsonLen = decLen;
    }

    // Parse response and merge
    std::vector<ExoStorage::RemoteTag> remoteTags;
    std::vector<Row>                   remoteRows;
    std::vector<Tombstone>             remoteDeletedRows;
    std::vector<Tombstone>             remoteDeletedTags;
    int64_t serverTS = 0;

    bool ok = parseResponse(jsonBuf, jsonLen,
                             remoteTags, remoteRows,
                             remoteDeletedRows, remoteDeletedTags,
                             serverTS);
    if (decBuf) { free(decBuf); decBuf = nullptr; }
    if (respBuf) { free(respBuf); respBuf = nullptr; }

    if (!ok) {
        statusOut = "JSON parse error";
        return SyncResult::ERR_JSON;
    }

    g_storage.applyRemote(remoteTags, remoteRows, remoteDeletedRows, remoteDeletedTags);

    // Advance timestamps
    if (serverTS == 0) {
        struct timeval tv; gettimeofday(&tv, nullptr);
        serverTS = (int64_t)tv.tv_sec * 1000000000LL + (int64_t)tv.tv_usec * 1000LL;
    }
    g_storage.lastSyncTS = serverTS;

    int64_t maxSentTS = 0;
    for (const Row& r : localRows)
        if (r.updatedTS > maxSentTS) maxSentTS = r.updatedTS;
    if (maxSentTS > g_storage.lastPushTS)
        g_storage.lastPushTS = maxSentTS;

    g_storage.save();
    statusOut = "Sync OK (" + String(remoteRows.size()) + " rows in)";
    return SyncResult::OK;
}
