package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"time"
)

var eventTypeNames = map[string]string{
	"cannot_return":   "无法还车",
	"no_bike":         "站点无车",
	"brake_fault":     "刹车异常",
	"lock_stuck":      "锁具打不开",
	"truck_stuck":     "调拨车堵在路上",
	"tidal_imbalance": "潮汐失衡",
}

// createEvent 建立协同事件，并按事件类型自动拉入相关角色。
func createEvent(evType, title string, stationID int64, rideID, bikeID, taskID *int64, creatorID int64, priority string) (int64, error) {
	var eventID int64
	err := db.QueryRow(`INSERT INTO events(type,title,station_id,ride_id,bike_id,task_id,priority,created_by)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		evType, title, stationID, rideID, bikeID, taskID, priority, creatorID).Scan(&eventID)
	if err != nil {
		return 0, err
	}
	// 参与角色矩阵：把用户、客服、调度员、维修员、站点管理员放进同一事件
	roleSet := map[string]bool{"cs": true}
	switch evType {
	case "cannot_return", "no_bike":
		roleSet["dispatcher"] = true
		roleSet["station_admin"] = true
	case "brake_fault", "lock_stuck":
		roleSet["repair"] = true
	case "truck_stuck":
		roleSet["dispatcher"] = true
	case "tidal_imbalance":
		roleSet["dispatcher"] = true
		roleSet["station_admin"] = true
		roleSet["operator"] = true
		roleSet["city"] = true
	}
	add := func(userID int64, role string) {
		db.Exec(`INSERT INTO event_participants(event_id,user_id,role) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`,
			eventID, userID, role)
	}
	add(creatorID, "creator")
	rows, err := db.Query(`SELECT id, role FROM users WHERE role=ANY($1::text[])`, "{"+joinKeys(roleSet)+"}")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var uid int64
			var role string
			if rows.Scan(&uid, &role) == nil {
				add(uid, role)
			}
		}
	}
	db.Exec(`INSERT INTO event_messages(event_id,sender_id,content,msg_type)
		VALUES($1,NULL,$2,'system')`, eventID,
		fmt.Sprintf("事件「%s」已创建，类型：%s，相关人员已加入协同处理", title, eventTypeNames[evType]))
	return eventID, nil
}

func joinKeys(m map[string]bool) string {
	s := ""
	for k := range m {
		if s != "" {
			s += ","
		}
		s += k
	}
	return s
}

func eventsHandler(w http.ResponseWriter, r *http.Request, u *User) {
	status := r.URL.Query().Get("status")
	q := `
		SELECT e.id, e.type, e.title, COALESCE(s.name,''), e.status, e.priority,
		       e.created_at, e.resolved_at,
		       (SELECT count(*) FROM event_messages m WHERE m.event_id=e.id),
		       (SELECT count(*) FROM event_participants p WHERE p.event_id=e.id)
		FROM events e LEFT JOIN stations s ON s.id=e.station_id`
	args := []any{}
	where := ""
	// 用户只能看到自己参与的事件；员工可见全部
	if u.Role == "user" {
		where = ` WHERE EXISTS(SELECT 1 FROM event_participants ep WHERE ep.event_id=e.id AND ep.user_id=$1)`
		args = append(args, u.ID)
	}
	if status != "" && status != "all" {
		if where == "" {
			where = " WHERE"
		} else {
			where += " AND"
		}
		where += fmt.Sprintf(" e.status=$%d", len(args)+1)
		args = append(args, status)
	}
	q += where + ` ORDER BY e.id DESC LIMIT 100`
	rows, err := db.Query(q, args...)
	if err != nil {
		writeErr(w, 500, "查询事件失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var (
			id, msgs, parts      int64
			typ, title, st, pri  string
			station              string
			created              time.Time
			resolved             sql.NullTime
		)
		if err := rows.Scan(&id, &typ, &title, &station, &st, &pri, &created, &resolved, &msgs, &parts); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id, "type": typ, "type_name": eventTypeNames[typ], "title": title,
			"station": station, "status": st, "priority": pri, "created_at": created,
			"resolved_at": timePtr(resolved), "message_count": msgs, "participant_count": parts,
		})
	}
	writeJSON(w, 200, out)
}

func createEventHandler(w http.ResponseWriter, r *http.Request, u *User) {
	var req struct {
		Type      string `json:"type"`
		Title     string `json:"title"`
		StationID int64  `json:"station_id"`
		RideID    *int64 `json:"ride_id"`
		BikeID    *int64 `json:"bike_id"`
		TaskID    *int64 `json:"task_id"`
	}
	if err := decodeBody(r, &req); err != nil || req.Type == "" || req.StationID == 0 {
		writeErr(w, 400, "参数不完整")
		return
	}
	if _, ok := eventTypeNames[req.Type]; !ok {
		writeErr(w, 400, "未知事件类型")
		return
	}
	if req.Title == "" {
		var name string
		db.QueryRow(`SELECT name FROM stations WHERE id=$1`, req.StationID).Scan(&name)
		req.Title = fmt.Sprintf("%s（%s）", eventTypeNames[req.Type], name)
	}
	priority := "medium"
	if req.Type == "brake_fault" || req.Type == "cannot_return" {
		priority = "high"
	}
	eventID, err := createEvent(req.Type, req.Title, req.StationID, req.RideID, req.BikeID, req.TaskID, u.ID, priority)
	if err != nil {
		writeErr(w, 500, "创建事件失败")
		return
	}
	writeJSON(w, 200, map[string]any{"event_id": eventID, "message": "事件已创建，相关人员已加入"})
}

func eventDetailHandler(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	var (
		typ, title, status, pri, resolution string
		station                             sql.NullString
		rideID, bikeID, taskID              sql.NullInt64
		bikeCode                            sql.NullString
		created                             time.Time
		resolved                            sql.NullTime
	)
	err := db.QueryRow(`
		SELECT e.type, e.title, COALESCE(s.name,''), e.status, e.priority, COALESCE(e.resolution,''),
		       e.ride_id, e.bike_id, e.task_id, COALESCE(b.code,''), e.created_at, e.resolved_at
		FROM events e
		LEFT JOIN stations s ON s.id=e.station_id
		LEFT JOIN bikes b ON b.id=e.bike_id
		WHERE e.id=$1`, id).Scan(&typ, &title, &station, &status, &pri, &resolution,
		&rideID, &bikeID, &taskID, &bikeCode, &created, &resolved)
	if err == sql.ErrNoRows {
		writeErr(w, 404, "事件不存在")
		return
	} else if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	// 参与者
	participants := []map[string]any{}
	prows, err := db.Query(`SELECT p.user_id, u.name, u.role, p.joined_at
		FROM event_participants p JOIN users u ON u.id=p.user_id WHERE p.event_id=$1 ORDER BY p.id`, id)
	if err == nil {
		defer prows.Close()
		for prows.Next() {
			var (
				uid    int64
				name   string
				role   string
				joined time.Time
			)
			if prows.Scan(&uid, &name, &role, &joined) == nil {
				participants = append(participants, map[string]any{
					"user_id": uid, "name": name, "role": role,
					"role_name": roleNames[role], "joined_at": joined,
				})
			}
		}
	}
	// 消息时间线
	messages := []map[string]any{}
	mrows, err := db.Query(`SELECT m.id, COALESCE(u.name,'系统'), COALESCE(u.role,'system'), m.content, m.msg_type, m.created_at
		FROM event_messages m LEFT JOIN users u ON u.id=m.sender_id
		WHERE m.event_id=$1 ORDER BY m.id`, id)
	if err == nil {
		defer mrows.Close()
		for mrows.Next() {
			var (
				mid                  int64
				name, role, content  string
				mt                   string
				at                   time.Time
			)
			if mrows.Scan(&mid, &name, &role, &content, &mt, &at) == nil {
				messages = append(messages, map[string]any{
					"id": mid, "sender": name, "role": role, "role_name": roleNames[role],
					"content": content, "msg_type": mt, "created_at": at,
				})
			}
		}
	}
	writeJSON(w, 200, map[string]any{
		"id": id, "type": typ, "type_name": eventTypeNames[typ], "title": title,
		"station": station.String, "status": status, "priority": pri, "resolution": resolution,
		"ride_id": nullInt(rideID), "bike_id": nullInt(bikeID), "bike_code": bikeCode.String,
		"task_id": nullInt(taskID), "created_at": created, "resolved_at": timePtr(resolved),
		"participants": participants, "messages": messages,
	})
}

func eventJoinHandler(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	_, err := db.Exec(`INSERT INTO event_participants(event_id,user_id,role) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`,
		id, u.ID, u.Role)
	if err != nil {
		writeErr(w, 500, "加入失败")
		return
	}
	db.Exec(`INSERT INTO event_messages(event_id,sender_id,content,msg_type) VALUES($1,$2,$3,'system')`,
		id, u.ID, fmt.Sprintf("%s（%s）加入事件协同", u.Name, u.RoleName))
	writeJSON(w, 200, map[string]any{"ok": true})
}

func eventMessageHandler(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	var req struct {
		Content string `json:"content"`
	}
	if err := decodeBody(r, &req); err != nil || req.Content == "" {
		writeErr(w, 400, "内容不能为空")
		return
	}
	// 参与者或员工才能发言
	var allowed bool
	if staffRoles[u.Role] {
		allowed = true
	} else {
		db.QueryRow(`SELECT EXISTS(SELECT 1 FROM event_participants WHERE event_id=$1 AND user_id=$2)`,
			id, u.ID).Scan(&allowed)
	}
	if !allowed {
		writeErr(w, 403, "仅事件参与人员可发言")
		return
	}
	if _, err := db.Exec(`INSERT INTO event_messages(event_id,sender_id,content) VALUES($1,$2,$3)`,
		id, u.ID, req.Content); err != nil {
		writeErr(w, 500, "发送失败")
		return
	}
	db.Exec(`UPDATE events SET status='processing' WHERE id=$1 AND status='open'`, id)
	writeJSON(w, 200, map[string]any{"ok": true})
}

// eventActionHandler: 事件内处置动作（生成调拨 / 派单维修 / 减免费用），动作写入时间线。
func eventActionHandler(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	var req struct {
		Action  string `json:"action"`
		Payload map[string]any `json:"payload"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, 400, "参数错误")
		return
	}
	var evType, title string
	var stationID sql.NullInt64
	var rideID, bikeID sql.NullInt64
	err := db.QueryRow(`SELECT type, title, station_id, ride_id, bike_id FROM events WHERE id=$1`, id).
		Scan(&evType, &title, &stationID, &rideID, &bikeID)
	if err != nil {
		writeErr(w, 404, "事件不存在")
		return
	}
	desc := ""
	switch req.Action {
	case "create_rebalance":
		toF, _ := req.Payload["to_station_id"].(float64)
		cntF, _ := req.Payload["bike_count"].(float64)
		if toF == 0 || cntF == 0 || !stationID.Valid {
			writeErr(w, 400, "调拨参数不完整")
			return
		}
		var taskID int64
		err := db.QueryRow(`INSERT INTO rebalance_tasks(from_station_id,to_station_id,bike_count,status,reason,explanation,created_by)
			VALUES($1,$2,$3,'pending',$4,$5,$6) RETURNING id`,
			stationID.Int64, int64(toF), int64(cntF),
			"事件处置："+title,
			fmt.Sprintf("由事件 #%s 处置生成：%s。发起角色：%s。", id, title, u.RoleName),
			u.ID).Scan(&taskID)
		if err != nil {
			writeErr(w, 500, "创建调拨任务失败")
			return
		}
		desc = fmt.Sprintf("已创建调拨任务 #%d：从本站调出 %d 辆", taskID, int64(cntF))
	case "dispatch_repair":
		if !bikeID.Valid {
			writeErr(w, 400, "事件未关联车辆")
			return
		}
		res, err := db.Exec(`UPDATE faults SET status='assigned' WHERE bike_id=$1 AND status='pending'`, bikeID.Int64)
		if err != nil {
			writeErr(w, 500, "派单失败")
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			desc = "车辆故障单已在处理中"
		} else {
			desc = "已为该车派发维修工单"
		}
	case "waive_fee":
		if !rideID.Valid {
			writeErr(w, 400, "事件未关联行程")
			return
		}
		var fee sql.NullFloat64
		var userID int64
		db.QueryRow(`SELECT COALESCE(fee,0), user_id FROM rides WHERE id=$1`, rideID.Int64).Scan(&fee, &userID)
		if fee.Float64 <= 0 {
			writeErr(w, 400, "该行程无费用可减免")
			return
		}
		tx, ok := mustTx(w)
		if !ok {
			return
		}
		defer tx.Rollback()
		tx.Exec(`UPDATE users SET balance=balance+$1 WHERE id=$2`, fee.Float64, userID)
		tx.Exec(`UPDATE rides SET fee=0 WHERE id=$1`, rideID.Int64)
		if err := tx.Commit(); err != nil {
			writeErr(w, 500, "减免失败")
			return
		}
		desc = fmt.Sprintf("已为用户减免行程费用 %.2f 元", fee.Float64)
	default:
		writeErr(w, 400, "未知处置动作")
		return
	}
	db.Exec(`INSERT INTO event_messages(event_id,sender_id,content,msg_type) VALUES($1,$2,$3,'action')`,
		id, u.ID, fmt.Sprintf("%s（%s）：%s", u.Name, u.RoleName, desc))
	writeJSON(w, 200, map[string]any{"ok": true, "message": desc})
}

func eventResolveHandler(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	var req struct {
		Resolution string `json:"resolution"`
	}
	if err := decodeBody(r, &req); err != nil || req.Resolution == "" {
		writeErr(w, 400, "请填写处理结论")
		return
	}
	res, err := db.Exec(`UPDATE events SET status='resolved', resolution=$1, resolved_at=now()
		WHERE id=$2 AND status<>'resolved'`, req.Resolution, id)
	if err != nil {
		writeErr(w, 500, "操作失败")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, 409, "事件不存在或已解决")
		return
	}
	db.Exec(`INSERT INTO event_messages(event_id,sender_id,content,msg_type) VALUES($1,$2,$3,'system')`,
		id, u.ID, fmt.Sprintf("事件已解决：%s", req.Resolution))
	writeJSON(w, 200, map[string]any{"ok": true})
}
