#include "storage.h"
#include "uuid.h"
#include <ArduinoJson.h>
#include <LittleFS.h>
#include <sys/time.h>
#include <algorithm>

ExoStorage g_storage;

static const char* DATA_FILE = "/exo.json";

// ── Persistence ───────────────────────────────────────────────────────────────

bool ExoStorage::load() {
    if (!LittleFS.exists(DATA_FILE)) return true; // fresh start is OK

    File f = LittleFS.open(DATA_FILE, "r");
    if (!f) return false;

    JsonDocument doc;
    DeserializationError err = deserializeJson(doc, f);
    f.close();
    if (err) return false;

    lastSyncTS  = doc["lastSyncTS"]  | (int64_t)0;
    lastPushTS  = 0;  // always reset on boot — pre-NTP timestamps are unreliable
    lastTagUUID = doc["lastTagUUID"] | "";

    tags.clear();
    for (JsonObject o : doc["tags"].as<JsonArray>()) {
        Tag t;
        t.uuid      = o["uuid"] | "";
        t.name      = o["name"] | "";
        t.updatedTS = o["updatedTS"] | (int64_t)0;
        tags.push_back(std::move(t));
    }

    rows.clear();
    for (JsonObject o : doc["rows"].as<JsonArray>()) {
        Row r;
        r.uuid      = o["uuid"]    | "";
        r.tagUUID   = o["tagUUID"] | "";
        r.text      = o["text"]    | "";
        r.note      = o["note"]    | "";
        r.rank      = o["rank"]    | 0;
        r.updatedTS = o["updatedTS"] | (int64_t)0;
        r.priority  = o["priority"]  | 0;
        r.done      = o["done"]      | false;
        rows.push_back(std::move(r));
    }

    deletedRows.clear();
    for (JsonObject o : doc["deletedRows"].as<JsonArray>()) {
        Tombstone ts;
        ts.key       = o["key"]       | "";
        ts.deletedTS = o["deletedTS"] | (int64_t)0;
        deletedRows.push_back(std::move(ts));
    }

    deletedTags.clear();
    for (JsonObject o : doc["deletedTags"].as<JsonArray>()) {
        Tombstone ts;
        ts.key       = o["key"]       | "";
        ts.deletedTS = o["deletedTS"] | (int64_t)0;
        deletedTags.push_back(std::move(ts));
    }

    return true;
}

bool ExoStorage::save() {
    JsonDocument doc;

    doc["lastSyncTS"]  = lastSyncTS;
    // lastPushTS intentionally not saved — reset to 0 on every boot
    doc["lastTagUUID"] = lastTagUUID;

    JsonArray jTags = doc["tags"].to<JsonArray>();
    for (const Tag& t : tags) {
        JsonObject o = jTags.add<JsonObject>();
        o["uuid"]      = t.uuid;
        o["name"]      = t.name;
        o["updatedTS"] = t.updatedTS;
    }

    JsonArray jRows = doc["rows"].to<JsonArray>();
    for (const Row& r : rows) {
        JsonObject o = jRows.add<JsonObject>();
        o["uuid"]      = r.uuid;
        o["tagUUID"]   = r.tagUUID;
        o["text"]      = r.text;
        o["note"]      = r.note;
        o["rank"]      = r.rank;
        o["updatedTS"] = r.updatedTS;
        o["priority"]  = r.priority;
        o["done"]      = r.done;
    }

    JsonArray jDR = doc["deletedRows"].to<JsonArray>();
    for (const Tombstone& ts : deletedRows) {
        JsonObject o = jDR.add<JsonObject>();
        o["key"]       = ts.key;
        o["deletedTS"] = ts.deletedTS;
    }

    JsonArray jDT = doc["deletedTags"].to<JsonArray>();
    for (const Tombstone& ts : deletedTags) {
        JsonObject o = jDT.add<JsonObject>();
        o["key"]       = ts.key;
        o["deletedTS"] = ts.deletedTS;
    }

    File f = LittleFS.open(DATA_FILE, "w");
    if (!f) return false;
    serializeJson(doc, f);
    f.close();
    return true;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

int64_t ExoStorage::nowNanos() const {
    struct timeval tv;
    gettimeofday(&tv, nullptr);
    return (int64_t)tv.tv_sec * 1000000000LL + (int64_t)tv.tv_usec * 1000LL;
}

int ExoStorage::findTagByUUID(const String& uuid) const {
    for (int i = 0; i < (int)tags.size(); i++)
        if (tags[i].uuid == uuid) return i;
    return -1;
}

int ExoStorage::findTagByName(const String& name) const {
    for (int i = 0; i < (int)tags.size(); i++)
        if (tags[i].name == name) return i;
    return -1;
}

int ExoStorage::ensureTag(const String& name) {
    int idx = findTagByName(name);
    if (idx >= 0) return idx;
    Tag t;
    t.uuid      = generateUUID();
    t.name      = name;
    t.updatedTS = nowNanos();
    tags.push_back(std::move(t));
    return tags.size() - 1;
}

std::vector<int> ExoStorage::rowsForTag(const String& tagUUID) const {
    std::vector<int> result;
    for (int i = 0; i < (int)rows.size(); i++)
        if (rows[i].tagUUID == tagUUID) result.push_back(i);
    std::sort(result.begin(), result.end(), [&](int a, int b){
        const Row& ra = rows[a];
        const Row& rb = rows[b];
        // not-done before done
        if (ra.done != rb.done) return !ra.done;
        // priority 1-5 sorts before 0 (unset); mirror effectivePriority() in Go
        int pa = ra.priority == 0 ? 6 : ra.priority;
        int pb = rb.priority == 0 ? 6 : rb.priority;
        if (pa != pb) return pa < pb;
        return ra.rank < rb.rank;
    });
    return result;
}

Row* ExoStorage::addRow(const String& tagUUID, const String& text) {
    // Find max rank for this tag
    int32_t maxRank = -1;
    for (const Row& r : rows)
        if (r.tagUUID == tagUUID && r.rank > maxRank) maxRank = r.rank;

    Row r;
    r.uuid      = generateUUID();
    r.tagUUID   = tagUUID;
    r.text      = text;
    r.rank      = maxRank + 1;
    r.updatedTS = nowNanos();
    r.priority  = 0;
    r.done      = false;
    rows.push_back(std::move(r));
    return &rows.back();
}

bool ExoStorage::updateRowText(int rowIdx, const String& text) {
    if (rowIdx < 0 || rowIdx >= (int)rows.size()) return false;
    rows[rowIdx].text      = text;
    rows[rowIdx].updatedTS = nowNanos();
    return true;
}

bool ExoStorage::updateRowPriority(int rowIdx, int8_t priority) {
    if (rowIdx < 0 || rowIdx >= (int)rows.size()) return false;
    rows[rowIdx].priority  = priority;
    rows[rowIdx].updatedTS = nowNanos();
    return true;
}

bool ExoStorage::toggleDone(int rowIdx) {
    if (rowIdx < 0 || rowIdx >= (int)rows.size()) return false;
    rows[rowIdx].done      = !rows[rowIdx].done;
    rows[rowIdx].updatedTS = nowNanos();
    return true;
}

bool ExoStorage::swapRowRanks(int rowIdxA, int rowIdxB) {
    if (rowIdxA < 0 || rowIdxA >= (int)rows.size()) return false;
    if (rowIdxB < 0 || rowIdxB >= (int)rows.size()) return false;
    int64_t now = nowNanos();
    std::swap(rows[rowIdxA].rank, rows[rowIdxB].rank);
    rows[rowIdxA].updatedTS = now;
    rows[rowIdxB].updatedTS = now;
    return true;
}

bool ExoStorage::deleteTag(int tagIdx) {
    if (tagIdx < 0 || tagIdx >= (int)tags.size()) return false;
    String uuid = tags[tagIdx].uuid;
    Tombstone ts;
    ts.key       = uuid;
    ts.deletedTS = nowNanos();
    deletedTags.push_back(ts);
    rows.erase(std::remove_if(rows.begin(), rows.end(),
        [&](const Row& r){ return r.tagUUID == uuid; }), rows.end());
    tags.erase(tags.begin() + tagIdx);
    return true;
}

bool ExoStorage::deleteRow(int rowIdx) {
    if (rowIdx < 0 || rowIdx >= (int)rows.size()) return false;
    Tombstone ts;
    ts.key       = rows[rowIdx].uuid;
    ts.deletedTS = nowNanos();
    deletedRows.push_back(ts);
    rows.erase(rows.begin() + rowIdx);
    return true;
}

// ── Sync payload build ────────────────────────────────────────────────────────

void ExoStorage::buildPayload(int64_t since,
                              std::vector<Tag>&       outTags,
                              std::vector<Row>&       outRows,
                              std::vector<Tombstone>& outDeletedRows,
                              std::vector<Tombstone>& outDeletedTags) const
{
    // Always send all tags (server needs them to map IDs)
    outTags = tags;

    for (const Row& r : rows)
        if (r.updatedTS > since) outRows.push_back(r);

    for (const Tombstone& ts : deletedRows)
        if (ts.deletedTS > since) outDeletedRows.push_back(ts);

    for (const Tombstone& ts : deletedTags)
        if (ts.deletedTS > since) outDeletedTags.push_back(ts);
}

// ── Apply remote (LWW merge) ──────────────────────────────────────────────────

void ExoStorage::applyRemote(const std::vector<RemoteTag>&  remoteTags,
                              const std::vector<Row>&         remoteRows,
                              const std::vector<Tombstone>&   remoteDeletedRows,
                              const std::vector<Tombstone>&   remoteDeletedTags)
{
    // Step 1: Merge tags, build server-integer-ID → local-index map
    // (remoteRows carry the server's integer tag_id, not uuid)
    // We stored the server ID in RemoteTag.serverID.
    std::vector<std::pair<int64_t,int>> idMap; // serverID → local index

    for (const RemoteTag& rt : remoteTags) {
        const Tag& t = rt.tag;
        int idx = findTagByUUID(t.uuid);
        if (idx >= 0) {
            // Found by UUID: update name if remote is newer
            if (t.updatedTS > tags[idx].updatedTS) {
                tags[idx].name      = t.name;
                tags[idx].updatedTS = t.updatedTS;
            }
        } else {
            // Try by name (same tag created on two devices independently)
            idx = findTagByName(t.name);
            if (idx < 0) {
                // Check if we locally tombstoned this UUID — our deletion wins
                bool tombstoned = false;
                for (const Tombstone& ts : deletedTags)
                    if (ts.key == t.uuid && ts.deletedTS >= t.updatedTS) {
                        tombstoned = true;
                        break;
                    }
                if (!tombstoned) {
                    Tag nt = t;
                    tags.push_back(std::move(nt));
                    idx = tags.size() - 1;
                }
            }
            // If found by name, keep local UUID (server and client diverge on
            // UUID but agree on name - same heuristic as the Go server)
        }
        idMap.push_back({rt.serverID, idx});
    }

    auto resolveTagIdx = [&](int64_t serverTagID) -> int {
        for (auto& p : idMap)
            if (p.first == serverTagID) return p.second;
        return -1;
    };

    // Step 2: Merge rows (LWW by updatedTS, keyed by UUID)
    for (Row rr : remoteRows) {
        // Resolve tagUUID from server tag ID
        int tagIdx = resolveTagIdx(
            // remoteRows carry raw server tag_id; we stored it in Row.tagUUID
            // as a decimal string during JSON parsing (see sync.cpp)
            (int64_t)atoll(rr.tagUUID.c_str())
        );
        if (tagIdx < 0) continue;
        rr.tagUUID = tags[tagIdx].uuid;

        // Find local row by UUID
        int found = -1;
        for (int i = 0; i < (int)rows.size(); i++)
            if (rows[i].uuid == rr.uuid) { found = i; break; }

        if (found < 0) {
            rows.push_back(rr);
        } else if (rr.updatedTS > rows[found].updatedTS) {
            rows[found] = rr;
        }
    }

    // Step 3: Apply tombstones (after upserts, matching Go's order)
    auto applyRowTombstone = [&](const Tombstone& ts) {
        // Record tombstone (keep the newer one)
        bool found = false;
        for (Tombstone& existing : deletedRows) {
            if (existing.key == ts.key) {
                if (ts.deletedTS > existing.deletedTS) existing.deletedTS = ts.deletedTS;
                found = true;
                break;
            }
        }
        if (!found) deletedRows.push_back(ts);

        // Delete local row if tombstone is newer than row's updatedTS
        for (int i = 0; i < (int)rows.size(); i++) {
            if (rows[i].uuid == ts.key && ts.deletedTS > rows[i].updatedTS) {
                rows.erase(rows.begin() + i);
                break;
            }
        }
    };

    auto applyTagTombstone = [&](const Tombstone& ts) {
        bool found = false;
        for (Tombstone& existing : deletedTags) {
            if (existing.key == ts.key) {
                if (ts.deletedTS > existing.deletedTS) existing.deletedTS = ts.deletedTS;
                found = true;
                break;
            }
        }
        if (!found) deletedTags.push_back(ts);

        for (int i = 0; i < (int)tags.size(); i++) {
            if (tags[i].uuid == ts.key && ts.deletedTS > tags[i].updatedTS) {
                // Remove all rows belonging to this tag too
                rows.erase(std::remove_if(rows.begin(), rows.end(),
                    [&](const Row& r){ return r.tagUUID == ts.key; }), rows.end());
                tags.erase(tags.begin() + i);
                break;
            }
        }
    };

    for (const Tombstone& ts : remoteDeletedRows) applyRowTombstone(ts);
    for (const Tombstone& ts : remoteDeletedTags) applyTagTombstone(ts);
}
