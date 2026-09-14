package main

import (
	"database/sql"
	"fmt"
	"math"
	"net/http"
	"time"
)

const requiredDeposit = 199.0

// calcFee: 1.5 元 / 30 分钟，不足 30 分钟按 30 分钟计。
func calcFee(borrow, ret time.Time) float64 {
	mins := ret.Sub(borrow).Minutes()
	if mins < 1 {
		mins = 1
	}
	return math.Ceil(mins/30.0) * 1.5
}

// borrowHandler 核验：账户状态 → 骑行限制 → 进行中行程 → 押金 → 站点状态 → 电源 → 车辆编号/状态。
func borrowHandler(w http.ResponseWriter, r *http.Request, u *User) {
	var req struct {
		StationID int64  `json:"station_id"`
		BikeCode  string `json:"bike_code"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	// 1. 账户核验
	if u.Status != "active" {
		writeErr(w, 403, "账户已被冻结，请联系客服")
		return
	}
	// 2. 骑行限制
	if u.Restricted {
		writeErr(w, 403, "您已被限制骑行：存在未处理的违规/申诉记录，请联系客服解除")
		return
	}
	var ongoing int
	db.QueryRow(`SELECT count(*) FROM rides WHERE user_id=$1 AND status='ongoing'`, u.ID).Scan(&ongoing)
	if ongoing > 0 {
		writeErr(w, 409, "您有进行中的行程，请先还车")
		return
	}
	// 3. 押金核验
	if u.Deposit < requiredDeposit {
		writeErr(w, 403, fmt.Sprintf("押金不足：需缴纳 %.0f 元押金后方可借车（当前 %.0f 元）", requiredDeposit, u.Deposit))
		return
	}
	// 4. 站点状态核验
	var stStatus, stPower, stName string
	var nearFZ bool
	err := db.QueryRow(`SELECT status, power_status, name, near_forbidden_zone FROM stations WHERE id=$1`,
		req.StationID).Scan(&stStatus, &stPower, &stName, &nearFZ)
	if err == sql.ErrNoRows {
		writeErr(w, 404, "站点不存在")
		return
	} else if err != nil {
		writeErr(w, 500, "查询站点失败")
		return
	}
	if stStatus == "weather_suspended" {
		writeErr(w, 403, "恶劣天气停运中，该站点暂停借车（还车不受影响）")
		return
	}
	if stStatus != "normal" {
		writeErr(w, 403, "站点暂停服务")
		return
	}
	if stPower != "normal" {
		writeErr(w, 403, "站点电源故障，智能锁无法供电，暂无法借车")
		return
	}
	// 5. 车辆编号与状态核验
	var bikeID int64
	var bikeStatus, lockStatus string
	var bikeStation sql.NullInt64
	err = db.QueryRow(`SELECT id, status, lock_status, station_id FROM bikes WHERE code=$1`, req.BikeCode).
		Scan(&bikeID, &bikeStatus, &lockStatus, &bikeStation)
	if err == sql.ErrNoRows {
		writeErr(w, 404, "车辆编号不存在，请核对后重试")
		return
	} else if err != nil {
		writeErr(w, 500, "查询车辆失败")
		return
	}
	if !bikeStation.Valid || bikeStation.Int64 != req.StationID {
		writeErr(w, 409, "该车辆不在所选站点，请确认车辆编号")
		return
	}
	if bikeStatus != "docked" {
		writeErr(w, 409, "车辆当前不可借（故障/调度/维修中），请更换车辆")
		return
	}

	tx, ok := mustTx(w)
	if !ok {
		return
	}
	defer tx.Rollback()
	// 解锁车桩
	var dockID int64
	err = tx.QueryRow(`UPDATE docks SET status='empty', bike_id=NULL
		WHERE station_id=$1 AND bike_id=$2 RETURNING id`, req.StationID, bikeID).Scan(&dockID)
	if err != nil {
		writeErr(w, 409, "车桩状态异常，请重试")
		return
	}
	if _, err := tx.Exec(`UPDATE bikes SET status='rented', station_id=NULL, lock_status='unlocked' WHERE id=$1`, bikeID); err != nil {
		writeErr(w, 500, "开锁失败")
		return
	}
	var rideID int64
	err = tx.QueryRow(`INSERT INTO rides(user_id,bike_id,borrow_station_id,borrow_dock_id,status)
		VALUES($1,$2,$3,$4,'ongoing') RETURNING id`, u.ID, bikeID, req.StationID, dockID).Scan(&rideID)
	if err != nil {
		writeErr(w, 500, "创建行程失败")
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, 500, "提交失败")
		return
	}
	warning := ""
	if nearFZ {
		warning = "提示：该站点临近骑行禁区，请勿在禁骑区内骑行/停放"
	}
	writeJSON(w, 200, map[string]any{
		"ride_id": rideID, "bike_code": req.BikeCode, "station": stName,
		"message": "开锁成功，请取车", "warning": warning,
	})
}

// returnHandler 记录：桩位、车辆锁止、费用、故障反馈、用户位置。
func returnHandler(w http.ResponseWriter, r *http.Request, u *User) {
	rideID := r.PathValue("id")
	var req struct {
		StationID     int64  `json:"station_id"`
		FaultType     string `json:"fault_type"`
		FaultFeedback string `json:"fault_feedback"`
		Location      string `json:"location"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	var (
		bikeID      int64
		borrowTime  time.Time
		rideStatus  string
		rideUser    int64
	)
	err := db.QueryRow(`SELECT user_id, bike_id, borrow_time, status FROM rides WHERE id=$1`, rideID).
		Scan(&rideUser, &bikeID, &borrowTime, &rideStatus)
	if err == sql.ErrNoRows {
		writeErr(w, 404, "行程不存在")
		return
	} else if err != nil {
		writeErr(w, 500, "查询行程失败")
		return
	}
	if rideUser != u.ID {
		writeErr(w, 403, "只能结束自己的行程")
		return
	}
	if rideStatus != "ongoing" {
		writeErr(w, 409, "该行程已结束")
		return
	}
	var stName string
	var capUsed, capTotal int
	err = db.QueryRow(`SELECT name, capacity,
		(SELECT count(*) FROM docks d WHERE d.station_id=s.id AND d.status='occupied')
		FROM stations s WHERE s.id=$1`, req.StationID).Scan(&stName, &capTotal, &capUsed)
	if err != nil {
		writeErr(w, 404, "站点不存在")
		return
	}

	tx, ok := mustTx(w)
	if !ok {
		return
	}
	defer tx.Rollback()
	// 分配空闲桩位
	var dockID int64
	var dockNo int
	err = tx.QueryRow(`UPDATE docks SET status='occupied', bike_id=$1
		WHERE id=(SELECT id FROM docks WHERE station_id=$2 AND status='empty' ORDER BY dock_no LIMIT 1 FOR UPDATE)
		RETURNING id, dock_no`, bikeID, req.StationID).Scan(&dockID, &dockNo)
	if err == sql.ErrNoRows {
		// 满桩：返回 409，前端引导用户创建「无法还车」协同事件
		writeJSON(w, 409, map[string]any{
			"error":      "station_full",
			"message":    fmt.Sprintf("「%s」已满桩（%d/%d），可前往附近站点还车，或发起「无法还车」协同处理", stName, capUsed, capTotal),
			"station_id": req.StationID,
		})
		return
	} else if err != nil {
		writeErr(w, 500, "分配桩位失败")
		return
	}
	// 车辆锁止
	bikeNewStatus := "docked"
	if req.FaultType != "" {
		bikeNewStatus = "fault"
	}
	if _, err := tx.Exec(`UPDATE bikes SET status=$1, station_id=$2, lock_status='locked', total_rides=total_rides+1 WHERE id=$3`,
		bikeNewStatus, req.StationID, bikeID); err != nil {
		writeErr(w, 500, "车辆锁止失败")
		return
	}
	// 费用
	now := time.Now()
	fee := calcFee(borrowTime, now)
	if _, err := tx.Exec(`UPDATE rides SET return_station_id=$1, return_dock_id=$2, return_time=$3,
		fee=$4, status='completed', user_location=$5, fault_type=$6, fault_feedback=$7 WHERE id=$8`,
		req.StationID, dockID, now, fee, req.Location, req.FaultType, req.FaultFeedback, rideID); err != nil {
		writeErr(w, 500, "结束行程失败")
		return
	}
	if _, err := tx.Exec(`UPDATE users SET balance=balance-$1 WHERE id=$2`, fee, u.ID); err != nil {
		writeErr(w, 500, "扣费失败")
		return
	}
	// 故障反馈 → 生成故障单
	var faultID int64
	if req.FaultType != "" {
		err = tx.QueryRow(`INSERT INTO faults(bike_id,ride_id,station_id,type,description,status,reporter_id)
			VALUES($1,$2,$3,$4,$5,'pending',$6) RETURNING id`,
			bikeID, rideID, req.StationID, req.FaultType, req.FaultFeedback, u.ID).Scan(&faultID)
		if err != nil {
			writeErr(w, 500, "记录故障反馈失败")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, 500, "提交失败")
		return
	}
	resp := map[string]any{
		"message":  "还车成功，车辆已锁止",
		"dock_no":  dockNo,
		"fee":      fee,
		"duration": fmt.Sprintf("%.0f 分钟", now.Sub(borrowTime).Minutes()),
	}
	if faultID > 0 {
		resp["fault_id"] = faultID
		resp["message"] = "还车成功，车辆已锁止；故障反馈已受理，该车已暂停运营"
	}
	writeJSON(w, 200, resp)
}

func myRidesHandler(w http.ResponseWriter, r *http.Request, u *User) {
	rows, err := db.Query(`
		SELECT r.id, b.code, s1.name, r.borrow_time, COALESCE(s2.name,''), r.return_time,
		       COALESCE(r.fee,0), r.status, COALESCE(r.fault_type,''),
		       EXISTS(SELECT 1 FROM appeals a WHERE a.ride_id=r.id)
		FROM rides r
		JOIN bikes b ON b.id=r.bike_id
		JOIN stations s1 ON s1.id=r.borrow_station_id
		LEFT JOIN stations s2 ON s2.id=r.return_station_id
		WHERE r.user_id=$1 ORDER BY r.id DESC LIMIT 50`, u.ID)
	if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var (
			id                          int64
			bike, from, to, status, ft  string
			bt                          time.Time
			rt                          sql.NullTime
			fee                         float64
			hasAppeal                   bool
		)
		if err := rows.Scan(&id, &bike, &from, &bt, &to, &rt, &fee, &status, &ft, &hasAppeal); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"id": id, "bike_code": bike, "from_station": from, "borrow_time": bt,
			"to_station": to, "return_time": timePtr(rt), "fee": fee, "status": status,
			"fault_type": ft, "has_appeal": hasAppeal,
		})
	}
	writeJSON(w, 200, out)
}

// rideDetailHandler: 一次骑行的完整链路（借车桩位 → 还车状态 → 费用 → 故障 → 申诉）。
func rideDetailHandler(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	var (
		userID, bikeID                 int64
		bikeCode, status, loc, ft, ff  string
		fromName, toName               string
		fromDock, toDock               sql.NullInt64
		bt                             time.Time
		rt                             sql.NullTime
		fee                            sql.NullFloat64
	)
	err := db.QueryRow(`
		SELECT r.user_id, r.bike_id, b.code, r.status, r.user_location, r.fault_type, r.fault_feedback,
		       s1.name, COALESCE(s2.name,''), COALESCE(d1.dock_no,0), COALESCE(d2.dock_no,0),
		       r.borrow_time, r.return_time, r.fee
		FROM rides r
		JOIN bikes b ON b.id=r.bike_id
		JOIN stations s1 ON s1.id=r.borrow_station_id
		LEFT JOIN stations s2 ON s2.id=r.return_station_id
		LEFT JOIN docks d1 ON d1.id=r.borrow_dock_id
		LEFT JOIN docks d2 ON d2.id=r.return_dock_id
		WHERE r.id=$1`, id).Scan(&userID, &bikeID, &bikeCode, &status, &loc, &ft, &ff,
		&fromName, &toName, &fromDock, &toDock, &bt, &rt, &fee)
	if err == sql.ErrNoRows {
		writeErr(w, 404, "行程不存在")
		return
	} else if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	if u.Role == "user" && userID != u.ID {
		writeErr(w, 403, "无权查看他人行程")
		return
	}
	writeJSON(w, 200, map[string]any{
		"id": id, "user_id": userID, "bike_id": bikeID, "bike_code": bikeCode, "status": status,
		"borrow_station": fromName, "borrow_dock_no": fromDock.Int64, "borrow_time": bt,
		"return_station": toName, "return_dock_no": toDock.Int64, "return_time": timePtr(rt),
		"fee": fee.Float64, "user_location": loc, "fault_type": ft, "fault_feedback": ff,
	})
}

// lockStuckHandler: 用户上报「锁具打不开」→ 自动建立协同事件。
func lockStuckHandler(w http.ResponseWriter, r *http.Request, u *User) {
	rideIDStr := r.PathValue("id")
	var rideID, bikeID, stationID int64
	var bikeCode, stName string
	err := db.QueryRow(`SELECT r.id, r.bike_id, r.borrow_station_id, b.code, s.name
		FROM rides r JOIN bikes b ON b.id=r.bike_id JOIN stations s ON s.id=r.borrow_station_id
		WHERE r.id=$1 AND r.user_id=$2 AND r.status='ongoing'`, rideIDStr, u.ID).
		Scan(&rideID, &bikeID, &stationID, &bikeCode, &stName)
	if err != nil {
		writeErr(w, 404, "行程不存在或已结束")
		return
	}
	eventID, err := createEvent("lock_stuck", fmt.Sprintf("车锁无法打开（%s @ %s）", bikeCode, stName),
		stationID, &rideID, &bikeID, nil, u.ID, "high")
	if err != nil {
		writeErr(w, 500, "创建事件失败")
		return
	}
	writeJSON(w, 200, map[string]any{"event_id": eventID, "message": "已创建「锁具打不开」协同事件，客服与维修员已加入处理"})
}
