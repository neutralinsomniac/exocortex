CREATE TABLE IF NOT EXISTS "ref" (
	"tag_id"	INTEGER NOT NULL,
	"row_id"	INTEGER NOT NULL,
	FOREIGN KEY("row_id") REFERENCES "row"("id") ON DELETE CASCADE,
	PRIMARY KEY("tag_id","row_id"),
	FOREIGN KEY("tag_id") REFERENCES "tag"("id") ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS "tag" (
	"id"	INTEGER,
	"name"	TEXT NOT NULL UNIQUE,
	"refcount"	INTEGER NOT NULL DEFAULT 0,
	"updated_ts"	INTEGER DEFAULT 0,
	PRIMARY KEY("id")
);
CREATE TABLE IF NOT EXISTS "row" (
	"id"	INTEGER,
	"tag_id"	INTEGER NOT NULL,
	"rank"	INTEGER,
	"text"	BLOB,
	"parent_row_id"	INTEGER,
	"updated_ts"	INTEGER,
	PRIMARY KEY("id")
);
