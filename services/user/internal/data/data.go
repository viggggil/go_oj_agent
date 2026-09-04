package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	mysql "github.com/go-sql-driver/mysql"

	"github.com/viggggil/go_oj_agent/services/user/internal/biz"
)

var (
	_ biz.UserRepository = (*StoreSet)(nil)
	_ biz.RoleRepository = (*StoreSet)(nil)
)

type StoreSet struct {
	db *sql.DB
}

func NewStoreSet(db *sql.DB) *StoreSet {
	return &StoreSet{db: db}
}

func (s *StoreSet) CreateUser(
	ctx context.Context,
	user biz.User,
	defaultRole biz.RoleName,
) (created biz.User, err error) {
	if s == nil || s.db == nil {
		return biz.User{}, fmt.Errorf("user database is not configured")
	}

	// 创建用户和绑定默认角色必须在同一事务内完成，避免出现无角色用户。
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return biz.User{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO users (username, email, password_hash, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3))
	`, user.Username, user.Email, user.PasswordHash, user.Status)
	if err != nil {
		return biz.User{}, translateMySQLError(err)
	}

	user.ID, err = result.LastInsertId()
	if err != nil {
		return biz.User{}, err
	}

	var roleID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM roles WHERE name = ?`, defaultRole).Scan(&roleID)
	if errors.Is(err, sql.ErrNoRows) {
		return biz.User{}, fmt.Errorf("default role %q is not seeded", defaultRole)
	}
	if err != nil {
		return biz.User{}, err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO user_roles (user_id, role_id, created_at)
		VALUES (?, ?, UTC_TIMESTAMP(3))
	`, user.ID, roleID)
	if err != nil {
		return biz.User{}, err
	}

	if err = tx.Commit(); err != nil {
		return biz.User{}, err
	}
	return user, nil
}

func (s *StoreSet) BootstrapAdmin(ctx context.Context, user biz.User) (created biz.User, err error) {
	if s == nil || s.db == nil {
		return biz.User{}, fmt.Errorf("user database is not configured")
	}

	// bootstrap 需要同时检查现有管理员、创建用户和绑定 admin 角色，必须保持事务一致性。
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return biz.User{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var adminRoleID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM roles WHERE name = ?`, biz.RoleAdmin).Scan(&adminRoleID)
	if errors.Is(err, sql.ErrNoRows) {
		return biz.User{}, fmt.Errorf("admin role is not seeded")
	}
	if err != nil {
		return biz.User{}, err
	}

	// bootstrap 是一次性命令：如果系统中已有 admin 角色用户，直接拒绝，避免覆盖或增发管理员。
	var existing int
	err = tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM user_roles ur
		INNER JOIN roles r ON r.id = ur.role_id
		WHERE r.name = ?
	`, biz.RoleAdmin).Scan(&existing)
	if err != nil {
		return biz.User{}, err
	}
	if existing > 0 {
		return biz.User{}, biz.ErrAdminAlreadyExists
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO users (username, email, password_hash, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3))
	`, user.Username, user.Email, user.PasswordHash, user.Status)
	if err != nil {
		return biz.User{}, translateMySQLError(err)
	}

	user.ID, err = result.LastInsertId()
	if err != nil {
		return biz.User{}, err
	}
	user.Roles = []biz.RoleName{biz.RoleAdmin}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO user_roles (user_id, role_id, created_at)
		VALUES (?, ?, UTC_TIMESTAMP(3))
	`, user.ID, adminRoleID)
	if err != nil {
		return biz.User{}, err
	}

	if err = tx.Commit(); err != nil {
		return biz.User{}, err
	}
	return user, nil
}

func (s *StoreSet) FindByID(ctx context.Context, userID int64) (biz.User, error) {
	return s.findUser(ctx, `u.id = ?`, userID)
}

func (s *StoreSet) FindByAccount(ctx context.Context, account string) (biz.User, error) {
	return s.findUser(ctx, `(LOWER(u.username) = LOWER(?) OR LOWER(u.email) = LOWER(?))`, account, account)
}

func (s *StoreSet) ListUserRoles(ctx context.Context, userID int64) ([]biz.RoleName, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("user database is not configured")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT r.name
		FROM roles r
		INNER JOIN user_roles ur ON ur.role_id = r.id
		WHERE ur.user_id = ?
		ORDER BY r.name
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roles []biz.RoleName
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		roles = append(roles, biz.RoleName(name))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return roles, nil
}

func (s *StoreSet) findUser(ctx context.Context, predicate string, args ...any) (biz.User, error) {
	if s == nil || s.db == nil {
		return biz.User{}, fmt.Errorf("user database is not configured")
	}

	query := `
		SELECT u.id, u.username, u.email, u.password_hash, u.status,
		       r.name
		FROM users u
		LEFT JOIN user_roles ur ON ur.user_id = u.id
		LEFT JOIN roles r ON r.id = ur.role_id
		WHERE ` + predicate + `
		ORDER BY r.name
	`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return biz.User{}, err
	}
	defer rows.Close()

	var user biz.User
	found := false
	for rows.Next() {
		var role sql.NullString
		if err := rows.Scan(
			&user.ID,
			&user.Username,
			&user.Email,
			&user.PasswordHash,
			&user.Status,
			&role,
		); err != nil {
			return biz.User{}, err
		}
		found = true
		if role.Valid {
			user.Roles = append(user.Roles, biz.RoleName(role.String))
		}
	}
	if err := rows.Err(); err != nil {
		return biz.User{}, err
	}
	if !found {
		return biz.User{}, biz.ErrUserNotFound
	}
	return user, nil
}

func translateMySQLError(err error) error {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return biz.ErrUserAlreadyExists
	}
	return err
}
