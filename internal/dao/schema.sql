CREATE TABLE IF NOT EXISTS tasks (
    id TEXT PRIMARY KEY,
    semantic_hash TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS items (
    task_id TEXT NOT NULL,
    trace_id TEXT NOT NULL,
    input_index INTEGER NOT NULL,
    source_hash TEXT NOT NULL,
    raw_json BLOB NOT NULL,
    prompt TEXT NOT NULL,
    response TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('pending', 'processing', 'retry_wait', 'succeeded', 'failed')),
    request_attempts INTEGER NOT NULL DEFAULT 0,
    repair_attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TEXT,
    annotation BLOB,
    error_category TEXT,
    error_summary TEXT,
    PRIMARY KEY (task_id, trace_id),
    UNIQUE (task_id, input_index),
    FOREIGN KEY (task_id) REFERENCES tasks(id)
);

CREATE TABLE IF NOT EXISTS attempts (
    id INTEGER PRIMARY KEY,
    task_id TEXT NOT NULL,
    trace_id TEXT NOT NULL,
    phase TEXT NOT NULL,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    http_status INTEGER,
    error_category TEXT,
    retryable INTEGER NOT NULL DEFAULT 0 CHECK (retryable IN (0, 1)),
    raw_response TEXT,
    validation_error TEXT,
    prompt_tokens INTEGER,
    completion_tokens INTEGER,
    FOREIGN KEY (task_id, trace_id) REFERENCES items(task_id, trace_id)
);
