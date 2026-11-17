-- +goose Up
-- Memories point at their scope (a bot, a team or a person) by type and id,
-- with no foreign key, so deleting a bot, a group or a user left its memories
-- behind: unreachable, and still counted against nothing. Delete them with
-- their scope from now on, and remove the ones already left over.
-- +goose StatementBegin
CREATE FUNCTION delete_scoped_memories() RETURNS trigger AS $$
BEGIN
    DELETE FROM memories WHERE scope_type = TG_ARGV[0] AND scope_id = OLD.id;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER bots_delete_memories AFTER DELETE ON bots
    FOR EACH ROW EXECUTE FUNCTION delete_scoped_memories('bot');
CREATE TRIGGER teams_delete_memories AFTER DELETE ON teams
    FOR EACH ROW EXECUTE FUNCTION delete_scoped_memories('team');
CREATE TRIGGER users_delete_memories AFTER DELETE ON users
    FOR EACH ROW EXECUTE FUNCTION delete_scoped_memories('user');

DELETE FROM memories m
WHERE (m.scope_type = 'bot' AND NOT EXISTS (SELECT 1 FROM bots b WHERE b.id = m.scope_id))
   OR (m.scope_type = 'team' AND NOT EXISTS (SELECT 1 FROM teams t WHERE t.id = m.scope_id))
   OR (m.scope_type = 'user' AND NOT EXISTS (SELECT 1 FROM users u WHERE u.id = m.scope_id));

-- +goose Down
DROP TRIGGER IF EXISTS users_delete_memories ON users;
DROP TRIGGER IF EXISTS teams_delete_memories ON teams;
DROP TRIGGER IF EXISTS bots_delete_memories ON bots;
DROP FUNCTION IF EXISTS delete_scoped_memories();
