CREATE TABLE IF NOT EXISTS "row" (
	"id"	INTEGER NOT NULL,
	"tag_id"	INTEGER NOT NULL,
	"rank"	INTEGER,
	"text"	BLOB,
	"parent_row_id"	INTEGER,
	PRIMARY KEY("id")
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS "tag" (
	"id"	INTEGER NOT NULL,
	"name"	TEXT,
	PRIMARY KEY("id")
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS "ref" (
	"tag_id"	INTEGER NOT NULL,
	"row_id"	INTEGER NOT NULL,
	FOREIGN KEY("row_id") REFERENCES "row"("id"),
	PRIMARY KEY("tag_id","row_id"),
	FOREIGN KEY("tag_id") REFERENCES "tag"("id")
);
