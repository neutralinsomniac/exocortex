#pragma once
#include <Arduino.h>

struct Tag {
    String uuid;
    String name;
    int64_t updatedTS;
};

struct Row {
    String uuid;
    String tagUUID;   // owning tag uuid (we match by uuid, not int id)
    String text;
    String note;
    int32_t rank;
    int64_t updatedTS;
    int8_t  priority;
    bool    done;
};

struct Tombstone {
    String  key;       // uuid of the deleted row or tag
    int64_t deletedTS;
};
