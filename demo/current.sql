-- ============================================================
--  hazel demo: blog schema (current state)
--  This file is loaded into demo.db when the image is built.
-- ============================================================

CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE posts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users (id),
    title TEXT NOT NULL,
    body TEXT,
    status TEXT NOT NULL DEFAULT 'draft',
    views TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX posts_user_id ON posts (user_id);

CREATE TABLE tags (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE post_tags (
    post_id INTEGER NOT NULL REFERENCES posts (id),
    tag_id INTEGER NOT NULL REFERENCES tags (id),
    PRIMARY KEY (post_id, tag_id)
) WITHOUT ROWID;

-- No longer used; desired.sql drops it.
CREATE TABLE legacy_logs (
    id INTEGER PRIMARY KEY,
    message TEXT
);

CREATE VIEW published_posts AS
  SELECT id, title FROM posts WHERE status = 'published';

-- ---------- sample rows ----------

INSERT INTO users (id, name, email) VALUES
    (1, 'Alice', 'alice@example.com'),
    (2, 'Bob', 'bob@example.com');

INSERT INTO posts (user_id, title, body, status, views) VALUES
    (1, 'Hello, hazel', 'First post.', 'published', '120'),
    (1, 'Draft notes', NULL, 'draft', '0'),
    (2, 'SQLite tips', 'Use STRICT tables.', 'published', '42');

INSERT INTO tags (id, name) VALUES (1, 'sqlite'), (2, 'news');
INSERT INTO post_tags (post_id, tag_id) VALUES (1, 2), (3, 1);
INSERT INTO legacy_logs (message) VALUES ('old');
