package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type User struct {
	ID         int64   `json:"id"`
	Username   string  `json:"username"`
	Name       string  `json:"name"`
	Role       string  `json:"role"`
	RoleName   string  `json:"role_name"`
	Deposit    float64 `json:"deposit"`
	Balance    float64 `json:"balance"`
	Status     string  `json:"status"`
	Restricted bool    `json:"restricted"`
}

var roleNames = map[string]string{
	"user":          "用户",
	"cs":            "客服",
	"dispatcher":    "调度员",
	"repair":        "维修员",
	"station_admin": "站点管理员",
	"operator":      "运营",
	"city":          "城市管理方",
	"driver":        "调拨车司机",
	"repair_lead":   "维修主管",
}

// locCN 统一按 Asia/Shanghai 计算运营时段（早高峰/放学/商圈/预测），
// 与容器 UTC 解耦；tzdata 缺失时回退到固定 +8 区。
var locCN = func() *time.Location {
	l, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return l
}()

// hourCN 返回当前 Asia/Shanghai 小时（0-23）。
func hourCN() int {
	return time.Now().In(locCN).Hour()
}

var staffRoles = map[string]bool{
	"cs": true, "dispatcher": true, "repair": true,
	"station_admin": true, "operator": true, "city": true, "driver": true,
	"repair_lead": true,
}

type ctxKey string

const ctxUser ctxKey = "user"

type handlerFunc func(http.ResponseWriter, *http.Request, *User)

// handle registers a route. roles: empty = public; "any" = logged in;
// "staff" = any staff role; otherwise listed roles (operator implicitly allowed).
func handle(mux *http.ServeMux, pattern string, fn handlerFunc, roles ...string) {
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if len(roles) == 0 {
			fn(w, r, nil)
			return
		}
		u := currentUser(r)
		if u == nil {
			writeErr(w, 401, "未登录或登录已过期")
			return
		}
		allowed := false
		for _, role := range roles {
			switch role {
			case "any":
				allowed = true
			case "staff":
				if staffRoles[u.Role] {
					allowed = true
				}
			default:
				if u.Role == role || u.Role == "operator" {
					allowed = true
				}
			}
			if allowed {
				break
			}
		}
		if !allowed {
			writeErr(w, 403, "当前角色无权执行该操作")
			return
		}
		fn(w, r, u)
	})
}

func currentUser(r *http.Request) *User {
	token := ""
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		token = strings.TrimPrefix(h, "Bearer ")
	}
	if token == "" {
		if c, err := r.Cookie("token"); err == nil {
			token = c.Value
		}
	}
	if token == "" {
		return nil
	}
	var u User
	err := db.QueryRow(`SELECT u.id, u.username, u.name, u.role, u.deposit, u.balance, u.status, u.restricted
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token=$1 AND s.expires_at > now()`, token).
		Scan(&u.ID, &u.Username, &u.Name, &u.Role, &u.Deposit, &u.Balance, &u.Status, &u.Restricted)
	if err != nil {
		return nil
	}
	u.RoleName = roleNames[u.Role]
	return &u
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}

func decodeBody(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func hashPassword(pw string) string {
	sum := sha256.Sum256([]byte("bikeops-salt:" + pw))
	return hex.EncodeToString(sum[:])
}

func newToken() string {
	b := make([]byte, 24)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func nullStr(s sql.NullString) string {
	if s.Valid {
		return s.String
	}
	return ""
}

func nullInt(n sql.NullInt64) int64 {
	if n.Valid {
		return n.Int64
	}
	return 0
}

// scanMap runs a query expected to return a single row of columns and scans into dest.
func mustTx(w http.ResponseWriter) (*sql.Tx, bool) {
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		writeErr(w, 500, "数据库事务开启失败")
		return nil, false
	}
	return tx, true
}

func timePtr(t sql.NullTime) *time.Time {
	if t.Valid {
		return &t.Time
	}
	return nil
}
