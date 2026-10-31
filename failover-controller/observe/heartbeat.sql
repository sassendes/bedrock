CREATE TABLE IF NOT EXISTS bedrock_heartbeat (
    observer text PRIMARY KEY,
    nonce    text NOT NULL,
    at       timestamptz NOT NULL
);

DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'bedrock_observer') THEN
        CREATE ROLE bedrock_observer LOGIN;
    END IF;
END
$$;

REVOKE ALL ON bedrock_heartbeat FROM PUBLIC;
GRANT SELECT (observer, nonce), INSERT (observer, nonce, at), UPDATE (nonce, at) ON bedrock_heartbeat TO bedrock_observer;
GRANT EXECUTE ON FUNCTION pg_control_system() TO bedrock_observer;
GRANT EXECUTE ON FUNCTION pg_control_checkpoint() TO bedrock_observer;
