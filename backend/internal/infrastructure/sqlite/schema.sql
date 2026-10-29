CREATE TABLE IF NOT EXISTS events (
 id TEXT PRIMARY KEY,
 title TEXT NOT NULL,
 starts_at TEXT NOT NULL,
 venue TEXT NOT NULL,
 remaining INTEGER NOT NULL CHECK (remaining >= 0)
);
CREATE TABLE IF NOT EXISTS checkouts (
 id TEXT PRIMARY KEY,
 event_id TEXT NOT NULL REFERENCES events(id),
 email TEXT NOT NULL,
 participants TEXT NOT NULL,
 total INTEGER NOT NULL CHECK (total > 0),
 expires_at TEXT NOT NULL,
 simulate_sold_out INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS orders (
 id TEXT PRIMARY KEY,
 checkout_id TEXT NOT NULL UNIQUE REFERENCES checkouts(id),
 number TEXT NOT NULL,
 created_at TEXT NOT NULL
);
