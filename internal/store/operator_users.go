package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Ostsee-Developer/AegisPXE/internal/event"
	"github.com/Ostsee-Developer/AegisPXE/internal/fault"
	"github.com/Ostsee-Developer/AegisPXE/internal/idgen"
	"github.com/Ostsee-Developer/AegisPXE/internal/operatoridentity"
	"github.com/go-webauthn/webauthn/webauthn"
)

const webAuthnHandleBytes = 64

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *Store) ResolveOperatorUser(ctx context.Context, provider, subject, displayName, email, requestID string) (operatoridentity.User, bool, error) {
	provider = strings.TrimSpace(provider)
	subject = strings.TrimSpace(subject)
	displayName = strings.TrimSpace(displayName)
	email = strings.TrimSpace(email)
	if err := operatoridentity.ValidateExternalIdentity(provider, subject); err != nil {
		return operatoridentity.User{}, false, fault.New(fault.OperatorAuthenticationFailed, "external operator identity is invalid", err)
	}
	if displayName == "" {
		displayName = subject
	}
	if len(displayName) > 160 || len(email) > 254 || containsControl(displayName) || containsControl(email) {
		return operatoridentity.User{}, false, fault.New(fault.OperatorAuthenticationFailed, "external operator identity metadata is invalid", nil)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return operatoridentity.User{}, false, s.storageError("begin operator identity transaction", err)
	}
	defer tx.Rollback()

	user, err := operatorUserByExternalTx(ctx, tx, provider, subject)
	if err == nil {
		return user, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return operatoridentity.User{}, false, s.storageError("resolve operator identity", err)
	}

	userID, err := idgen.New("u_")
	if err != nil {
		return operatoridentity.User{}, false, s.storageError("generate operator user id", err)
	}
	handle := make([]byte, webAuthnHandleBytes)
	if _, err := rand.Read(handle); err != nil {
		return operatoridentity.User{}, false, s.storageError("generate webauthn user handle", err)
	}
	now := s.now().UTC()
	user = operatoridentity.User{
		ID:             userID,
		Provider:       provider,
		Subject:        subject,
		DisplayName:    displayName,
		Email:          email,
		Status:         operatoridentity.StatusPendingReview,
		WebAuthnHandle: handle,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO operator_users(id,provider,subject,display_name,email,role,status,webauthn_handle,created_at,updated_at,approved_at,approved_by)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		user.ID, user.Provider, user.Subject, user.DisplayName, user.Email, "", string(user.Status), user.WebAuthnHandle,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), "", ""); err != nil {
		return operatoridentity.User{}, false, s.storageError("insert operator user", err)
	}
	if err := appendEventTx(ctx, tx, event.Event{
		EntityType: event.EntityOperatorUser,
		EntityID:   user.ID,
		Type:       event.OperatorUserDiscovered,
		OccurredAt: now,
		RequestID:  strings.TrimSpace(requestID),
		Actor:      "system:identity",
		Message:    "trusted external identity discovered",
	}); err != nil {
		return operatoridentity.User{}, false, s.storageError("audit operator discovery", err)
	}
	if err := tx.Commit(); err != nil {
		return operatoridentity.User{}, false, s.storageError("commit operator identity", err)
	}
	s.logger.InfoContext(ctx, "operator identity discovered",
		"component", "store.operator_user",
		"operation", "resolve_external",
		"request_id", strings.TrimSpace(requestID),
		"user_id", user.ID,
		"provider", user.Provider,
		"external_subject", user.Subject,
		"status", user.Status,
		"result", "created",
	)
	return user, true, nil
}

func (s *Store) OperatorUserByExternalIdentity(ctx context.Context, provider, subject string) (operatoridentity.User, error) {
	user, err := operatorUserByExternalDB(ctx, s.db, strings.TrimSpace(provider), strings.TrimSpace(subject))
	if errors.Is(err, sql.ErrNoRows) {
		return operatoridentity.User{}, fault.New(fault.OperatorUserNotFound, "operator user not found", err)
	}
	if err != nil {
		return operatoridentity.User{}, s.storageError("query operator user", err)
	}
	return user, nil
}

func (s *Store) OperatorUser(ctx context.Context, userID string) (operatoridentity.User, error) {
	user, err := operatorUserByIDDB(ctx, s.db, strings.TrimSpace(userID))
	if errors.Is(err, sql.ErrNoRows) {
		return operatoridentity.User{}, fault.New(fault.OperatorUserNotFound, "operator user not found", err)
	}
	if err != nil {
		return operatoridentity.User{}, s.storageError("query operator user", err)
	}
	return user, nil
}

func (s *Store) OperatorUserForWebAuthn(ctx context.Context, userID, rpID string) (operatoridentity.User, error) {
	user, err := s.OperatorUser(ctx, userID)
	if err != nil {
		return operatoridentity.User{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT credential_json FROM operator_credentials WHERE user_id=? AND rp_id=? ORDER BY created_at`, user.ID, strings.TrimSpace(rpID))
	if err != nil {
		return operatoridentity.User{}, s.storageError("query operator credentials", err)
	}
	defer rows.Close()
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return operatoridentity.User{}, s.storageError("scan operator credential", err)
		}
		var credential webauthn.Credential
		if err := json.Unmarshal(payload, &credential); err != nil {
			return operatoridentity.User{}, s.storageError("decode operator credential", err)
		}
		user.Credentials = append(user.Credentials, credential)
	}
	if err := rows.Err(); err != nil {
		return operatoridentity.User{}, s.storageError("iterate operator credentials", err)
	}
	return user, nil
}

func (s *Store) OperatorUsers(ctx context.Context) ([]operatoridentity.User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,provider,subject,display_name,email,role,status,webauthn_handle,created_at,updated_at,approved_at,approved_by FROM operator_users ORDER BY created_at,id`)
	if err != nil {
		return nil, s.storageError("list operator users", err)
	}
	defer rows.Close()
	var users []operatoridentity.User
	for rows.Next() {
		user, err := scanOperatorUser(rows)
		if err != nil {
			return nil, s.storageError("scan operator user", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, s.storageError("iterate operator users", err)
	}
	return users, nil
}

func (s *Store) HasOperatorAdmin(ctx context.Context) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM operator_users WHERE role=? AND status<>?`, string(operatoridentity.RoleAdmin), string(operatoridentity.StatusBlocked)).Scan(&count); err != nil {
		return false, s.storageError("count operator admins", err)
	}
	return count > 0, nil
}

func (s *Store) ClaimInitialAdmin(ctx context.Context, userID, requestID, actor string) (operatoridentity.User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return operatoridentity.User{}, s.storageError("begin initial admin claim", err)
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM operator_users WHERE role='admin' AND status<>'blocked'`).Scan(&count); err != nil {
		return operatoridentity.User{}, s.storageError("count initial admins", err)
	}
	if count != 0 {
		return operatoridentity.User{}, fault.New(fault.OperatorAuthorizationDenied, "initial admin already exists", nil)
	}
	user, err := operatorUserByIDTx(ctx, tx, strings.TrimSpace(userID))
	if errors.Is(err, sql.ErrNoRows) {
		return operatoridentity.User{}, fault.New(fault.OperatorUserNotFound, "operator user not found", err)
	}
	if err != nil {
		return operatoridentity.User{}, s.storageError("load initial admin candidate", err)
	}
	if user.Status != operatoridentity.StatusPendingReview {
		return operatoridentity.User{}, fault.New(fault.OperatorAuthorizationDenied, "initial admin candidate is not pending", nil)
	}
	now := s.now().UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE operator_users SET role=?,status=?,approved_at=?,approved_by=?,updated_at=? WHERE id=?`,
		string(operatoridentity.RoleAdmin), string(operatoridentity.StatusEnrollmentRequired), now.Format(time.RFC3339Nano), strings.TrimSpace(actor), now.Format(time.RFC3339Nano), user.ID); err != nil {
		return operatoridentity.User{}, s.storageError("claim initial admin", err)
	}
	if err := appendEventTx(ctx, tx, event.Event{EntityType: event.EntityOperatorUser, EntityID: user.ID, Type: event.OperatorInitialAdminClaimed, OccurredAt: now, RequestID: strings.TrimSpace(requestID), Actor: strings.TrimSpace(actor), Message: "initial administrator claimed"}); err != nil {
		return operatoridentity.User{}, s.storageError("audit initial admin claim", err)
	}
	if err := tx.Commit(); err != nil {
		return operatoridentity.User{}, s.storageError("commit initial admin claim", err)
	}
	return s.OperatorUser(ctx, user.ID)
}

func (s *Store) ApproveOperatorUser(ctx context.Context, userID string, role operatoridentity.Role, requestID, actor string) (operatoridentity.User, error) {
	if err := operatoridentity.ValidateRole(role); err != nil {
		return operatoridentity.User{}, fault.New(fault.OperatorAuthorizationDenied, "operator role is invalid", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return operatoridentity.User{}, s.storageError("begin operator approval", err)
	}
	defer tx.Rollback()
	user, err := operatorUserByIDTx(ctx, tx, strings.TrimSpace(userID))
	if errors.Is(err, sql.ErrNoRows) {
		return operatoridentity.User{}, fault.New(fault.OperatorUserNotFound, "operator user not found", err)
	}
	if err != nil {
		return operatoridentity.User{}, s.storageError("load operator approval target", err)
	}
	if user.Status != operatoridentity.StatusPendingReview {
		return operatoridentity.User{}, fault.New(fault.OperatorAuthorizationDenied, "operator user is not pending review", nil)
	}
	now := s.now().UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE operator_users SET role=?,status=?,approved_at=?,approved_by=?,updated_at=? WHERE id=?`, string(role), string(operatoridentity.StatusEnrollmentRequired), now.Format(time.RFC3339Nano), strings.TrimSpace(actor), now.Format(time.RFC3339Nano), user.ID); err != nil {
		return operatoridentity.User{}, s.storageError("approve operator user", err)
	}
	if err := appendEventTx(ctx, tx, event.Event{EntityType: event.EntityOperatorUser, EntityID: user.ID, Type: event.OperatorUserApproved, OccurredAt: now, RequestID: strings.TrimSpace(requestID), Actor: strings.TrimSpace(actor), Message: "operator user approved as " + string(role)}); err != nil {
		return operatoridentity.User{}, s.storageError("audit operator approval", err)
	}
	if err := tx.Commit(); err != nil {
		return operatoridentity.User{}, s.storageError("commit operator approval", err)
	}
	s.logger.InfoContext(ctx, "operator user approved", "component", "store.operator_user", "operation", "approve", "request_id", strings.TrimSpace(requestID), "actor", strings.TrimSpace(actor), "user_id", user.ID, "role", role, "result", "success")
	return s.OperatorUser(ctx, user.ID)
}

func (s *Store) BlockOperatorUser(ctx context.Context, userID, requestID, actor string) (operatoridentity.User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return operatoridentity.User{}, s.storageError("begin operator block", err)
	}
	defer tx.Rollback()
	user, err := operatorUserByIDTx(ctx, tx, strings.TrimSpace(userID))
	if errors.Is(err, sql.ErrNoRows) {
		return operatoridentity.User{}, fault.New(fault.OperatorUserNotFound, "operator user not found", err)
	}
	if err != nil {
		return operatoridentity.User{}, s.storageError("load operator block target", err)
	}
	if user.Role == operatoridentity.RoleAdmin && user.Status != operatoridentity.StatusBlocked {
		var admins int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM operator_users WHERE role='admin' AND status<>'blocked'`).Scan(&admins); err != nil {
			return operatoridentity.User{}, s.storageError("count active admins", err)
		}
		if admins <= 1 {
			return operatoridentity.User{}, fault.New(fault.OperatorAuthorizationDenied, "last administrator cannot be blocked", nil)
		}
	}
	now := s.now().UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE operator_users SET status=?,updated_at=? WHERE id=?`, string(operatoridentity.StatusBlocked), now.Format(time.RFC3339Nano), user.ID); err != nil {
		return operatoridentity.User{}, s.storageError("block operator user", err)
	}
	if err := appendEventTx(ctx, tx, event.Event{EntityType: event.EntityOperatorUser, EntityID: user.ID, Type: event.OperatorUserBlocked, OccurredAt: now, RequestID: strings.TrimSpace(requestID), Actor: strings.TrimSpace(actor), Message: "operator user blocked"}); err != nil {
		return operatoridentity.User{}, s.storageError("audit operator block", err)
	}
	if err := tx.Commit(); err != nil {
		return operatoridentity.User{}, s.storageError("commit operator block", err)
	}
	s.logger.WarnContext(ctx, "operator user blocked", "component", "store.operator_user", "operation", "block", "request_id", strings.TrimSpace(requestID), "actor", strings.TrimSpace(actor), "user_id", user.ID, "result", "success")
	return s.OperatorUser(ctx, user.ID)
}

func (s *Store) SaveOperatorCredential(ctx context.Context, userID, rpID string, credential webauthn.Credential, requestID, actor string, enrollment bool) error {
	payload, err := json.Marshal(credential)
	if err != nil {
		return s.storageError("encode operator credential", err)
	}
	now := s.now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return s.storageError("begin operator credential save", err)
	}
	defer tx.Rollback()
	user, err := operatorUserByIDTx(ctx, tx, strings.TrimSpace(userID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fault.New(fault.OperatorUserNotFound, "operator user not found", err)
		}
		return s.storageError("load credential owner", err)
	}
	if enrollment && user.Status != operatoridentity.StatusEnrollmentRequired {
		return fault.New(fault.OperatorAuthorizationDenied, "operator user is not awaiting passkey enrollment", nil)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO operator_credentials(user_id,rp_id,credential_id,credential_json,created_at,updated_at,last_used_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(user_id,rp_id,credential_id) DO UPDATE SET credential_json=excluded.credential_json,updated_at=excluded.updated_at,last_used_at=excluded.last_used_at`,
		user.ID, strings.TrimSpace(rpID), credential.ID, payload, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		return s.storageError("save operator credential", err)
	}
	if enrollment {
		if _, err := tx.ExecContext(ctx, `UPDATE operator_users SET status=?,updated_at=? WHERE id=?`, string(operatoridentity.StatusActive), now.Format(time.RFC3339Nano), user.ID); err != nil {
			return s.storageError("activate operator user", err)
		}
		if err := appendEventTx(ctx, tx, event.Event{EntityType: event.EntityOperatorUser, EntityID: user.ID, Type: event.OperatorPasskeyEnrolled, OccurredAt: now, RequestID: strings.TrimSpace(requestID), Actor: strings.TrimSpace(actor), Message: "operator passkey enrolled"}); err != nil {
			return s.storageError("audit passkey enrollment", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return s.storageError("commit operator credential", err)
	}
	s.logger.InfoContext(ctx, "operator credential persisted", "component", "store.operator_user", "operation", "credential", "request_id", strings.TrimSpace(requestID), "user_id", user.ID, "rp_id", strings.TrimSpace(rpID), "enrollment", enrollment, "result", "success")
	return nil
}

func operatorUserByExternalDB(ctx context.Context, db *sql.DB, provider, subject string) (operatoridentity.User, error) {
	return scanOperatorUser(db.QueryRowContext(ctx, `SELECT id,provider,subject,display_name,email,role,status,webauthn_handle,created_at,updated_at,approved_at,approved_by FROM operator_users WHERE provider=? AND subject=?`, provider, subject))
}

func operatorUserByExternalTx(ctx context.Context, tx *sql.Tx, provider, subject string) (operatoridentity.User, error) {
	return scanOperatorUser(tx.QueryRowContext(ctx, `SELECT id,provider,subject,display_name,email,role,status,webauthn_handle,created_at,updated_at,approved_at,approved_by FROM operator_users WHERE provider=? AND subject=?`, provider, subject))
}

func operatorUserByIDDB(ctx context.Context, db *sql.DB, userID string) (operatoridentity.User, error) {
	return scanOperatorUser(db.QueryRowContext(ctx, `SELECT id,provider,subject,display_name,email,role,status,webauthn_handle,created_at,updated_at,approved_at,approved_by FROM operator_users WHERE id=?`, userID))
}

func operatorUserByIDTx(ctx context.Context, tx *sql.Tx, userID string) (operatoridentity.User, error) {
	return scanOperatorUser(tx.QueryRowContext(ctx, `SELECT id,provider,subject,display_name,email,role,status,webauthn_handle,created_at,updated_at,approved_at,approved_by FROM operator_users WHERE id=?`, userID))
}

func scanOperatorUser(scanner rowScanner) (operatoridentity.User, error) {
	var user operatoridentity.User
	var role, status, created, updated, approved string
	if err := scanner.Scan(&user.ID, &user.Provider, &user.Subject, &user.DisplayName, &user.Email, &role, &status, &user.WebAuthnHandle, &created, &updated, &approved, &user.ApprovedBy); err != nil {
		return operatoridentity.User{}, err
	}
	user.Role = operatoridentity.Role(role)
	user.Status = operatoridentity.Status(status)
	var err error
	if user.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return operatoridentity.User{}, err
	}
	if user.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return operatoridentity.User{}, err
	}
	if approved != "" {
		if user.ApprovedAt, err = time.Parse(time.RFC3339Nano, approved); err != nil {
			return operatoridentity.User{}, err
		}
	}
	return user, nil
}

func containsControl(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}
