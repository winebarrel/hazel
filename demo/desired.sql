-- ============================================================
--  hazel demo: blog schema (desired state)
--  Edit this file freely, then run:
--    hazel plan demo.db desired.sql
-- ============================================================

-- Renamed name to display_name, and added bio.
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    -- hazel:renamed-from name
    display_name TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE,
    bio TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- views is now an INTEGER, which needs a rebuild of the table
-- (--allow-table-rebuild); added updated_at and a CHECK on status.
CREATE TABLE posts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users (id),
    title TEXT NOT NULL,
    body TEXT,
    status TEXT NOT NULL DEFAULT 'draft',
    views INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT,
    CONSTRAINT posts_status CHECK (status IN ('draft', 'published', 'archived'))
);

CREATE INDEX posts_user_id ON posts (user_id);

-- A partial index on the published posts.
CREATE INDEX posts_published ON posts (created_at) WHERE status = 'published';

CREATE TRIGGER posts_touch AFTER UPDATE ON posts
FOR EACH ROW WHEN NEW.updated_at IS OLD.updated_at
BEGIN
  UPDATE posts SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

CREATE TABLE tags (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE post_tags (
    post_id INTEGER NOT NULL REFERENCES posts (id),
    tag_id INTEGER NOT NULL REFERENCES tags (id),
    PRIMARY KEY (post_id, tag_id)
) WITHOUT ROWID;

-- New table.
CREATE TABLE comments (
    id INTEGER PRIMARY KEY,
    post_id INTEGER NOT NULL REFERENCES posts (id),
    body TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
) STRICT;

CREATE INDEX comments_post_id ON comments (post_id);

-- legacy_logs is gone: the plan drops it, with a WARNING.

-- The view now shows the author too.
CREATE VIEW published_posts AS
  SELECT p.id, p.title, u.display_name AS author
  FROM posts AS p JOIN users AS u ON u.id = p.user_id
  WHERE p.status = 'published';

-- Data the schema needs, added once.
-- hazel:execute SELECT NOT EXISTS (SELECT 1 FROM tags WHERE name = 'hazel')
INSERT INTO tags (name) VALUES ('hazel');
