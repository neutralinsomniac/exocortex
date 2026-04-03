#pragma once
#include <Arduino.h>
#include <vector>
#include "model.h"

// In-memory store + LittleFS persistence.
// All data lives in RAM; call save() to flush to /exo.json.
class ExoStorage {
public:
    std::vector<Tag>       tags;
    std::vector<Row>       rows;
    std::vector<Tombstone> deletedRows;
    std::vector<Tombstone> deletedTags;

    int64_t lastSyncTS = 0;   // server_ts from last sync response
    int64_t lastPushTS = 0;   // max updated_ts we last pushed
    String  lastTagUUID;       // reopen on boot

    bool load();
    bool save();

    // ── Tag helpers ──────────────────────────────────────────────────────────
    // Returns index into tags[], or -1.
    int findTagByUUID(const String& uuid) const;
    int findTagByName(const String& name) const;
    // Find or create; returns index.
    int ensureTag(const String& name);

    // ── Row helpers ──────────────────────────────────────────────────────────
    // Returns indices of rows for the given tag UUID, sorted by rank.
    std::vector<int> rowsForTag(const String& tagUUID) const;

    Row*  addRow(const String& tagUUID, const String& text);
    bool  updateRowText(int rowIdx, const String& text);
    bool  updateRowPriority(int rowIdx, int8_t priority);
    bool  toggleDone(int rowIdx);
    bool  deleteRow(int rowIdx);
    bool  deleteTag(int tagIdx);
    bool  renameTag(int tagIdx, const String& newName);
    bool  swapRowRanks(int rowIdxA, int rowIdxB);

    // ── Sync ─────────────────────────────────────────────────────────────────
    // Build the payload the client sends to the server.
    // All tags + rows/tombstones with updatedTS > since are included.
    void buildPayload(int64_t since,
                      std::vector<Tag>&       outTags,
                      std::vector<Row>&       outRows,
                      std::vector<Tombstone>& outDeletedRows,
                      std::vector<Tombstone>& outDeletedTags) const;

    // Apply what the server sent back (LWW merge).
    // remoteTagIDMap: server's integer tag ID → remote Tag (so we can resolve
    // row.tagID → tag uuid without embedding full struct in every row).
    struct RemoteTag { int64_t serverID; Tag tag; };
    void applyRemote(const std::vector<RemoteTag>&  remoteTags,
                     const std::vector<Row>&         remoteRows,
                     const std::vector<Tombstone>&   remoteDeletedRows,
                     const std::vector<Tombstone>&   remoteDeletedTags);

private:
    int64_t nowNanos() const;
};

extern ExoStorage g_storage;
