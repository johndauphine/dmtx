-- Immutable reconstruction of the first SQLite state layout, before typed
-- checkpoints, endpoint identity, target fencing, and Stage 2/4 evidence.
CREATE TABLE runs (
    id TEXT NOT NULL,
    source TEXT NOT NULL,
    target TEXT NOT NULL,
    outcome TEXT NOT NULL,
    resumable INTEGER NOT NULL,
    reason TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    ended_at DATETIME,
    PRIMARY KEY (id, outcome)
);
CREATE TABLE tasks (
    run_id TEXT NOT NULL,
    table_name TEXT NOT NULL,
    status TEXT NOT NULL,
    rows_done INTEGER NOT NULL,
    started_at DATETIME NOT NULL,
    completed_at DATETIME,
    PRIMARY KEY (run_id, table_name)
);
INSERT INTO runs VALUES (
    'completed-v0', 'source.db', 'target.db', 'success', 0, 'completed',
    '2025-01-02T03:04:05Z', '2025-01-02T03:05:05Z'
);
INSERT INTO tasks VALUES (
    'completed-v0', 'accounts', 'completed', 2,
    '2025-01-02T03:04:05Z', '2025-01-02T03:05:00Z'
);
INSERT INTO runs VALUES (
    'ambiguous-v0', 'source.db', 'target.db', 'failed', 1,
    'interrupted before typed checkpoint identity',
    '2025-01-03T03:04:05Z', '2025-01-03T03:05:05Z'
);
INSERT INTO tasks VALUES (
    'ambiguous-v0', 'accounts', 'running', 1,
    '2025-01-03T03:04:05Z', NULL
);
