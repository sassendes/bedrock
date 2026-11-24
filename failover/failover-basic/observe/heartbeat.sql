CREATE SCHEMA IF NOT EXISTS bedrock;
REVOKE ALL ON SCHEMA bedrock FROM PUBLIC;

DO $$
BEGIN
    IF to_regclass('public.bedrock_heartbeat') IS NOT NULL AND to_regclass('bedrock.bedrock_heartbeat') IS NULL THEN
        ALTER TABLE public.bedrock_heartbeat SET SCHEMA bedrock;
    END IF;
    IF to_regclass('public.bedrock_proof_key') IS NOT NULL AND to_regclass('bedrock.bedrock_proof_key') IS NULL THEN
        ALTER TABLE public.bedrock_proof_key SET SCHEMA bedrock;
    END IF;
END
$$;
DROP FUNCTION IF EXISTS public.bedrock_prove(text, text);
DROP FUNCTION IF EXISTS public.bedrock_receiver();

CREATE TABLE IF NOT EXISTS bedrock.bedrock_heartbeat (
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

GRANT USAGE ON SCHEMA bedrock TO bedrock_observer;
REVOKE ALL ON bedrock.bedrock_heartbeat FROM PUBLIC;
GRANT SELECT (observer, nonce), INSERT (observer, nonce, at), UPDATE (nonce, at) ON bedrock.bedrock_heartbeat TO bedrock_observer;
GRANT EXECUTE ON FUNCTION pg_control_system() TO bedrock_observer;
GRANT EXECUTE ON FUNCTION pg_control_checkpoint() TO bedrock_observer;

CREATE EXTENSION IF NOT EXISTS pgcrypto SCHEMA public;

CREATE TABLE IF NOT EXISTS bedrock.bedrock_proof_key (
    id     text PRIMARY KEY,
    key    bytea NOT NULL,
    active boolean NOT NULL DEFAULT false
);
REVOKE ALL ON bedrock.bedrock_proof_key FROM PUBLIC;

CREATE OR REPLACE FUNCTION bedrock.bedrock_prove(challenge text, observer text)
RETURNS TABLE (key_id text, instance text, system_id text, timeline bigint, lsn text, in_recovery boolean, at_ms bigint, mac text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE
    k record;
    payload text;
BEGIN
    SELECT id, key INTO k FROM bedrock.bedrock_proof_key WHERE active ORDER BY id DESC LIMIT 1;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'no active proof key';
    END IF;
    key_id := k.id;
    instance := coalesce(current_setting('cluster_name', true), '') || '@' || coalesce(host(inet_server_addr()), 'local');
    system_id := (SELECT s.system_identifier::text FROM pg_control_system() s);
    timeline := (SELECT c.timeline_id FROM pg_control_checkpoint() c);
    in_recovery := pg_is_in_recovery();
    lsn := CASE WHEN in_recovery THEN pg_last_wal_replay_lsn()::text ELSE pg_current_wal_lsn()::text END;
    at_ms := (extract(epoch FROM clock_timestamp()) * 1000)::bigint;
    payload := concat_ws('|', key_id, challenge, observer, instance, system_id, timeline::text, lsn, CASE WHEN in_recovery THEN 'true' ELSE 'false' END, at_ms::text);
    mac := encode(public.hmac(convert_to(payload, 'UTF8'), k.key, 'sha256'::text), 'hex');
    RETURN NEXT;
END
$$;
REVOKE ALL ON FUNCTION bedrock.bedrock_prove(text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION bedrock.bedrock_prove(text, text) TO bedrock_observer;

CREATE OR REPLACE FUNCTION bedrock.bedrock_receiver()
RETURNS TABLE (status text, age_ms bigint)
LANGUAGE sql SECURITY DEFINER SET search_path = pg_catalog AS $$
    SELECT r.status, (extract(epoch FROM clock_timestamp() - r.last_msg_receipt_time) * 1000)::bigint
    FROM pg_stat_wal_receiver r
$$;
REVOKE ALL ON FUNCTION bedrock.bedrock_receiver() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION bedrock.bedrock_receiver() TO bedrock_observer;
