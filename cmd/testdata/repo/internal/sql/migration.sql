DROP TABLE users;

CREATE TABLE users (
    id TEXT NO NULL PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
)
