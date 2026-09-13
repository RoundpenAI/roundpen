-- The per-user agent engine preference went away when Agent became Docker-only
-- (no engine picker, no qemu/kern choice); nothing reads or writes the table.
DROP TABLE IF EXISTS user_runtime;
