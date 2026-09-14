package main

import (
	"net/http"
	"time"
)

func loginHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	var u User
	var hash string
	err := db.QueryRow(`SELECT id,username,name,role,deposit,balance,status,restricted,password_hash
		FROM users WHERE username=$1`, req.Username).
		Scan(&u.ID, &u.Username, &u.Name, &u.Role, &u.Deposit, &u.Balance, &u.Status, &u.Restricted, &hash)
	if err != nil || hash != hashPassword(req.Password) {
		writeErr(w, 401, "用户名或密码错误")
		return
	}
	token := newToken()
	_, err = db.Exec(`INSERT INTO sessions(token,user_id,expires_at) VALUES($1,$2,$3)`,
		token, u.ID, time.Now().Add(72*time.Hour))
	if err != nil {
		writeErr(w, 500, "登录失败")
		return
	}
	u.RoleName = roleNames[u.Role]
	writeJSON(w, 200, map[string]any{"token": token, "user": u})
}

func logoutHandler(w http.ResponseWriter, r *http.Request, u *User) {
	h := r.Header.Get("Authorization")
	if len(h) > 7 {
		db.Exec(`DELETE FROM sessions WHERE token=$1`, h[7:])
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func meHandler(w http.ResponseWriter, r *http.Request, u *User) {
	writeJSON(w, 200, u)
}
