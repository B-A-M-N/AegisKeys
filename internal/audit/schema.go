package audit

import "fmt"

var eventSchemas = map[string]map[string]bool{
	"vault_initialized":                 {},
	"provider_edited":                   {},
	"provider_removed":                  {},
	"provider_added":                    {},
	"test_event":                        {},
	"credential_rotation_requested":     {"binding_ref": true, "client_executable": true},
	"credential_resolve_requested":      {"binding_ref": true, "client_executable": true},
	"credential_access_requested":       {"binding_ref": true, "client_executable": true, "operation": true},
	"broker_started":                    {"socket": true},
	"profile_deleted":                   {},
	"profile_added":                     {},
	"profile_created":                   {},
	"key_renamed":                       {},
	"key_deleted":                       {},
	"key_rotated":                       {},
	"key_added":                         {},
	"handoff_created":                   {},
	"envfile_created":                   {},
	"vault_item_rekeyed":                {},
	"vault_item_rotated":                {},
	"vault_item_linked":                 {},
	"vault_item_unlinked":               {},
	"vault_item_renamed":                {},
	"vault_item_deleted":                {},
	"vault_item_archived":               {},
	"vault_item_added":                  {},
	"secret_env_exported":               {},
	"secret_revealed":                   {},
	"secret_copied":                     {},
	"model_catalog_save":                {},
	"key.edit":                          {},
	"provider.edit":                     {},
	"key.add":                           {},
	"key.rotate":                        {},
	"key.replace":                       {},
	"key.delete":                        {},
	"profile.delete":                    {},
	"provider.delete":                   {},
	"profile.add":                       {},
	"provider.add":                      {},
	"scratch.copy_selection":            {},
	"scratch.copy":                      {},
	"scratch.create":                    {},
	"model_catalog_refresh":             {},
	"scratch.update":                    {},
	"scratch.delete":                    {},
	"profile.edit":                      {},
	"credential_resolved":               {"binding_ref": true, "client_executable": true},
	"credential_rotate_requested":       {"binding_ref": true, "client_executable": true},
	"credential_rotated":                {"binding_ref": true, "client_executable": true},
	"credential_access_denied":          {"binding_ref": true, "client_executable": true, "operation": true},
	"credential_request_rejected":       {"binding_ref": true, "client_executable": true, "operation": true},
	"broker_peer_authentication_failed": {"result": true},
	"broker_stopped":                    {"result": true},
	"broker_locked":                     {"result": true},
	"credential_binding_created":        {"binding_id": true},
	"credential_binding_rebound":        {"binding_id": true, "old_secret_id": true, "new_secret_id": true},
	"credential_binding_deleted":        {"binding_id": true},
	"credential_access_granted":         {"binding_id": true, "grant_id": true},
	"credential_access_revoked":         {"binding_id": true, "grant_id": true},
	"vault_rekeyed":                     {"reason": true, "old_time": true, "new_time": true},
	"child_start_failed":                {"duration_ms": true},
	"child_started":                     {"pid": true, "stdin_tty": true, "stdout_tty": true, "stderr_tty": true},
	"child_exited":                      {"pid": true, "exit_code": true, "signal": true, "duration_ms": true},
}

func validateEvent(e Event) error {
	if e.Event == "" || len(e.Event) > 128 {
		return fmt.Errorf("invalid audit event name")
	}
	for name, value := range map[string]string{"provider": e.Provider, "profile": e.Profile, "command": e.Command} {
		if len(value) > 4096 {
			return fmt.Errorf("audit %s field too long", name)
		}
	}
	if len(e.Metadata) > 64 {
		return fmt.Errorf("audit metadata has too many fields")
	}
	schema, known := eventSchemas[e.Event]
	for key, value := range e.Metadata {
		if key == "" || len(key) > 128 || len(value) > 4096 || containsControl(key) {
			return fmt.Errorf("invalid audit metadata field")
		}
		if known && key == "result" {
			continue
		}
		if known && !schema[key] {
			return fmt.Errorf("metadata %q is not valid for audit event %q", key, e.Event)
		}
	}
	return nil
}

func containsControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}
