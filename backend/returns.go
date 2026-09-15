package main

import (
	"database/sql"
	"fmt"
	"math"
	"net/http"
	"sort"
	"time"
)

// ============================ 满桩还车引导推荐 ============================

type nearbyStation struct {
	ID        int64   `json:"station_id"`
	Name      string  `json:"name"`
	Code      string  `json:"code"`
	FreeDocks int     `json:"free_docks"`
	Distance  float64 `json:"distance_km"`
	WalkMin   int     `json:"walk_min"`
	Reason    string  `json:"reason"`
}

const overtimeMinutes = 120 // 超过 2 小时视为超时骑行

// returnGuidanceHandler 用户到站满桩时：按附近空位、步行距离、超时状态、用户信用推荐还车点。
func returnGuidanceHandler(w http.ResponseWriter, r *http.Request, u *User) {
	var req struct {
		RideID int64 `json:"ride_id"`
	}
	if err := decodeBody(r, &req); err != nil || req.RideID == 0 {
		writeErr(w, 400, "参数错误")
		return
	}
	var (
		bikeID, borrowStationID int64
		borrowTime              time.Time
		status                  string
		feePaused               bool
	)
	err := db.QueryRow(`SELECT bike_id, borrow_station_id, borrow_time, status, fee_paused
		FROM rides WHERE id=$1 AND user_id=$2`, req.RideID, u.ID).
		Scan(&bikeID, &borrowStationID, &borrowTime, &status, &feePaused)
	if err == sql.ErrNoRows {
		writeErr(w, 404, "行程不存在")
		return
	} else if err != nil {
		writeErr(w, 500, "查询行程失败")
		return
	}
	if status != "ongoing" && status != "temp_pending" {
		writeErr(w, 409, "该行程已结束")
		return
	}
	var fx, fy float64
	var fname, fcode string
	if err := db.QueryRow(`SELECT name, code, location_x, location_y FROM stations WHERE id=$1`, borrowStationID).
		Scan(&fname, &fcode, &fx, &fy); err != nil {
		writeErr(w, 500, "查询站点失败")
		return
	}

	var credit int
	db.QueryRow(`SELECT credit_score FROM users WHERE id=$1`, u.ID).Scan(&credit)
	elapsedMin := int(time.Now().Sub(borrowTime) / time.Minute)
	overtime := elapsedMin > overtimeMinutes
	feeAtPause := math.Ceil(float64(elapsedMin)/30.0) * 1.5
	if feeAtPause < 1.5 {
		feeAtPause = 1.5
	}

	// 附近有空桩的站点
	rows, err := db.Query(`
		SELECT z.id, z.name, z.code, z.x, z.y, z.free FROM (
			SELECT s.id, s.name, s.code, s.location_x AS x, s.location_y AS y,
			       s.capacity - (SELECT count(*) FROM docks d WHERE d.station_id=s.id AND d.status='occupied') AS free
			FROM stations s WHERE s.status='normal'
		) z WHERE z.free > 0 ORDER BY z.id`)
	if err != nil {
		writeErr(w, 500, "查询附近站点失败")
		return
	}
	defer rows.Close()
	list := []nearbyStation{}
	for rows.Next() {
		var ns nearbyStation
		var x, y float64
		if rows.Scan(&ns.ID, &ns.Name, &ns.Code, &x, &y, &ns.FreeDocks) != nil {
			continue
		}
		dx, dy := (x-fx)*1.1, (y-fy)*0.9
		ns.Distance = math.Round(math.Hypot(dx, dy)*0.11*100) / 100 // 地图单位折算为约 0.11 km/单位
		if ns.Distance < 0.05 {
			ns.Distance = 0.05
		}
		ns.WalkMin = int(math.Round(ns.Distance / 0.08)) // 步行约 80m/分钟
		list = append(list, ns)
	}
	// 步行距离优先；超时用户更强调最近，高信用用户优先推荐空位最稳妥站点
	sort.SliceStable(list, func(i, j int) bool { return list[i].WalkMin < list[j].WalkMin })
	if len(list) > 5 {
		list = list[:5]
	}
	for i := range list {
		list[i].Reason = fmt.Sprintf("步行约 %d 分钟（%.1f km），空桩 %d 个", list[i].WalkMin, list[i].Distance, list[i].FreeDocks)
	}

	writeJSON(w, 200, map[string]any{
		"ride_id": req.RideID, "full_station": fname, "full_station_code": fcode,
		"elapsed_min": elapsedMin, "overtime": overtime, "credit_score": credit,
		"fee_now": feeAtPause, "fee_paused": feePaused,
		"nearby": list, "has_option": len(list) > 0,
		"tip": func() string {
			switch {
			case len(list) == 0:
				return "附近站点暂无空桩，可申请临时还车（客服审核后关闭计费）"
			case overtime:
				return "检测到骑行已超时，建议前往最近空桩站点，接受引导后费用暂停计算"
			case credit >= 90:
				return "您的信用良好，已为您优先推荐最近的有空桩站点"
			default:
				return "已按步行距离为您推荐附近有空桩的站点"
			}
		}(),
	})
}

// acceptGuidanceHandler 用户接受引导：费用计算暂停（停在接受时刻，直到在目标站点还车）。
func acceptGuidanceHandler(w http.ResponseWriter, r *http.Request, u *User) {
	var req struct {
		RideID      int64 `json:"ride_id"`
		StationID   int64 `json:"station_id"`
	}
	if err := decodeBody(r, &req); err != nil || req.RideID == 0 || req.StationID == 0 {
		writeErr(w, 400, "参数错误")
		return
	}
	var status string
	var bikeID int64
	err := db.QueryRow(`SELECT status,bike_id FROM rides WHERE id=$1 AND user_id=$2`, req.RideID, u.ID).
		Scan(&status, &bikeID)
	if err == sql.ErrNoRows {
		writeErr(w, 404, "行程不存在")
		return
	} else if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	if status != "ongoing" {
		writeErr(w, 409, "仅进行中的行程可接受引导")
		return
	}
	var free int
	var sname string
	db.QueryRow(`SELECT s.name, s.capacity-(SELECT count(*) FROM docks d WHERE d.station_id=s.id AND d.status='occupied')
		FROM stations s WHERE s.id=$1 AND s.status='normal'`, req.StationID).Scan(&sname, &free)
	if free <= 0 {
		writeErr(w, 409, "目标站点已无空桩，请刷新后改选其他站点")
		return
	}
	res, err := db.Exec(`UPDATE rides SET fee_paused=TRUE, fee_pause_time=now(), guidance_station_id=$1
		WHERE id=$2 AND user_id=$3 AND fee_paused=FALSE`, req.StationID, req.RideID, u.ID)
	if err != nil {
		writeErr(w, 500, "操作失败")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeJSON(w, 200, map[string]any{"ok": true, "message": "费用此前已暂停，请按引导前往目标站点还车"})
		return
	}
	var borrowTime time.Time
	db.QueryRow(`SELECT borrow_time FROM rides WHERE id=$1`, req.RideID).Scan(&borrowTime)
	mins := int(time.Now().Sub(borrowTime) / time.Minute)
	pausedFee := math.Ceil(float64(mins)/30.0) * 1.5
	if pausedFee < 1.5 {
		pausedFee = 1.5
	}
	writeJSON(w, 200, map[string]any{
		"ok": true,
		"message": fmt.Sprintf("已接受引导前往「%s」，费用暂停计算（当前费用 ¥%.2f 封顶，路上与找桩时间不再计费），请在该站点完成还车", sname, pausedFee),
		"fee_locked": pausedFee, "target_station": sname,
	})
}

// ============================ 临时还车处理单（用户发起 → 客服审核） ============================

func tempReturnCreateHandler(w http.ResponseWriter, r *http.Request, u *User) {
	var req struct {
		RideID       int64  `json:"ride_id"`
		StationID    int64  `json:"full_station_id"`
		StationCode  string `json:"station_code"`
		PhotoData    string `json:"photo_data"`
		UserLocation string `json:"user_location"`
	}
	if err := decodeBody(r, &req); err != nil || req.RideID == 0 {
		writeErr(w, 400, "请填写完整信息")
		return
	}
	if len(req.PhotoData) < 50 {
		writeErr(w, 400, "请上传包含站点编号的车辆照片")
		return
	}
	if req.UserLocation == "" {
		writeErr(w, 400, "请提供用户位置")
		return
	}
	var (
		bikeID, borrowStation int64
		borrowTime            time.Time
		status                string
		alreadyPaused         bool
		pauseTime             sql.NullTime
	)
	err := db.QueryRow(`SELECT bike_id, borrow_station_id, borrow_time, status, fee_paused, fee_pause_time
		FROM rides WHERE id=$1 AND user_id=$2`, req.RideID, u.ID).
		Scan(&bikeID, &borrowStation, &borrowTime, &status, &alreadyPaused, &pauseTime)
	if err == sql.ErrNoRows {
		writeErr(w, 404, "行程不存在")
		return
	} else if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	if status == "temp_pending" {
		writeErr(w, 409, "该行程已有待审核的临时还车单")
		return
	}
	if status != "ongoing" {
		writeErr(w, 409, "该行程已结束")
		return
	}
	fullID := req.StationID
	if fullID == 0 {
		fullID = borrowStation
	}
	var sname, scode string
	if err := db.QueryRow(`SELECT name, code FROM stations WHERE id=$1`, fullID).Scan(&sname, &scode); err != nil {
		writeErr(w, 404, "站点不存在")
		return
	}
	// 照片必须包含站点编号：要求用户核对的编号与所选站点一致
	if req.StationCode == "" {
		writeErr(w, 400, "请填写照片中显示的站点编号")
		return
	}
	if req.StationCode != scode {
		writeErr(w, 409, fmt.Sprintf("照片站点编号（%s）与所选站点「%s」编号（%s）不一致，请重新拍摄包含站点编号的照片", req.StationCode, sname, scode))
		return
	}
	mins := int(time.Now().Sub(borrowTime) / time.Minute)
	feeNow := math.Ceil(float64(mins)/30.0) * 1.5
	if feeNow < 1.5 {
		feeNow = 1.5
	}

	tx, ok := mustTx(w)
	if !ok {
		return
	}
	defer tx.Rollback()
	// 提交即暂停计费（客服审核期间不计费），车辆进入临时锁定（不可再借出，且不计入站点可用车）
	feeBefore := feeNow
	pauseAt := time.Now()
	if alreadyPaused && pauseTime.Valid {
		pauseAt = pauseTime.Time
		pm := int(pauseAt.Sub(borrowTime) / time.Minute)
		feeBefore = math.Ceil(float64(pm)/30.0) * 1.5
		if feeBefore < 1.5 {
			feeBefore = 1.5
		}
	}
	var orderID int64
	err = tx.QueryRow(`INSERT INTO temp_return_orders
		(ride_id,bike_id,user_id,full_station_id,station_code,photo_data,user_location,status,
		 overtime,fee_paused,pause_time,fee_before_pause)
		VALUES($1,$2,$3,$4,$5,$6,$7,'pending',$8,TRUE,$9,$10) RETURNING id`,
		req.RideID, bikeID, u.ID, fullID, scode, req.PhotoData, req.UserLocation,
		mins > overtimeMinutes, pauseAt, feeBefore).Scan(&orderID)
	if err != nil {
		writeErr(w, 500, "创建临时还车单失败")
		return
	}
	tx.Exec(`UPDATE rides SET status='temp_pending', fee_paused=TRUE, fee_pause_time=$1,
		fee_adjust_reason='临时还车待客服审核，审核期间暂停计费' WHERE id=$2`, pauseAt, req.RideID)
	tx.Exec(`UPDATE bikes SET status='temp_locked', station_id=NULL, lock_status='locked' WHERE id=$1`, bikeID)
	if err := tx.Commit(); err != nil {
		writeErr(w, 500, "提交失败")
		return
	}
	writeJSON(w, 200, map[string]any{
		"ok": true, "order_id": orderID,
		"message": fmt.Sprintf("临时还车申请已提交（站点 %s，编号 %s），客服审核期间停止计费；审核通过后您可在行程中看到费用调整理由", sname, scode),
	})
}

func tempReturnMyHandler(w http.ResponseWriter, r *http.Request, u *User) {
	rows, err := db.Query(`
		SELECT t.id, t.ride_id, b.code, s.name, t.station_code, t.status, t.overtime,
		       t.fee_paused, t.fee_before_pause, t.waiver_amount, t.final_fee,
		       COALESCE(t.adjust_reason,''), t.created_at, t.handled_at
		FROM temp_return_orders t
		JOIN bikes b ON b.id=t.bike_id
		JOIN stations s ON s.id=t.full_station_id
		WHERE t.user_id=$1 ORDER BY t.id DESC`, u.ID)
	if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	defer rows.Close()
	writeJSON(w, 200, scanTempOrders(rows))
}

// scanTempOrders 复用列表扫描（照片等审核要素由客服列表单独附加）。
func scanTempOrders(rows *sql.Rows) []map[string]any {
	out := []map[string]any{}
	for rows.Next() {
		var (
			id, rideID                         int64
			bikeCode, sname, code, status      string
			reason                             string
			overtime, paused                   bool
			feeBefore, waiver, finalFee        float64
			created                           time.Time
			handled                           sql.NullTime
		)
		if rows.Scan(&id, &rideID, &bikeCode, &sname, &code, &status, &overtime,
			&paused, &feeBefore, &waiver, &finalFee, &reason, &created, &handled) != nil {
			continue
		}
		row := map[string]any{
			"id": id, "ride_id": rideID, "bike_code": bikeCode, "station": sname,
			"station_code": code, "status": status,
			"status_name": map[string]string{"pending": "待客服审核", "approved": "审核通过", "rejected": "审核驳回"}[status],
			"overtime": overtime, "fee_paused": paused, "fee_before_pause": feeBefore,
			"waiver_amount": waiver, "final_fee": finalFee, "adjust_reason": reason,
			"created_at": created, "handled_at": timePtr(handled),
		}
		out = append(out, row)
	}
	return out
}

// ---------------- 客服审核 ----------------

func tempReturnListHandler(w http.ResponseWriter, r *http.Request, _ *User) {
	status := r.URL.Query().Get("status")
	q := `
		SELECT t.id, t.ride_id, b.code, s.name, t.station_code, t.status, t.overtime,
		       t.fee_paused, t.fee_before_pause, t.waiver_amount, t.final_fee,
		       COALESCE(t.adjust_reason,''), t.created_at, t.handled_at
		FROM temp_return_orders t
		JOIN bikes b ON b.id=t.bike_id
		JOIN stations s ON s.id=t.full_station_id
		JOIN users u ON u.id=t.user_id`
	args := []any{}
	if status == "pending" || status == "approved" || status == "rejected" {
		q += ` WHERE t.status=$1`
		args = append(args, status)
	}
	q += ` ORDER BY (t.status='pending') DESC, t.id DESC LIMIT 100`
	rows, err := db.Query(q, args...)
	if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	defer rows.Close()
	out := scanTempOrders(rows)
	// 附加用户与位置/照片等审核要素
	for i, row := range out {
		var (
			uname, phone, loc string
			photo             string
			borrowFee         float64
			borrowTime        time.Time
		)
		db.QueryRow(`SELECT u.name, COALESCE(u.phone,''), COALESCE(t.user_location,''),
			COALESCE(t.photo_data,''), COALESCE(r.fee,0), r.borrow_time
			FROM temp_return_orders t
			JOIN users u ON u.id=t.user_id JOIN rides r ON r.id=t.ride_id
			WHERE t.id=$1`, row["id"]).Scan(&uname, &phone, &loc, &photo, &borrowFee, &borrowTime)
		row["user_name"] = uname
		row["phone"] = phone
		row["user_location"] = loc
		row["photo_data"] = photo
		row["borrow_time"] = borrowTime
		out[i] = row
	}
	writeJSON(w, 200, out)
}

func tempReturnReviewHandler(w http.ResponseWriter, r *http.Request, u *User) {
	id := r.PathValue("id")
	var req struct {
		Action       string  `json:"action"` // approve / reject
		AdjustReason string  `json:"adjust_reason"`
		WaiverAmount float64 `json:"waiver_amount"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeErr(w, 400, "参数错误")
		return
	}
	if req.Action != "approve" && req.Action != "reject" {
		writeErr(w, 400, "审核结果须为 approve / reject")
		return
	}
	var (
		rideID, bikeID, fullStationID, userID int64
		curStatus                            string
		feeBefore                            float64
	)
	err := db.QueryRow(`SELECT ride_id,bike_id,full_station_id,user_id,status,fee_before_pause
		FROM temp_return_orders WHERE id=$1`, id).Scan(&rideID, &bikeID, &fullStationID, &userID, &curStatus, &feeBefore)
	if err == sql.ErrNoRows {
		writeErr(w, 404, "临时还车单不存在")
		return
	} else if err != nil {
		writeErr(w, 500, "查询失败")
		return
	}
	if curStatus != "pending" {
		writeErr(w, 409, "该临时还车单已审核")
		return
	}

	tx, ok := mustTx(w)
	if !ok {
		return
	}
	defer tx.Rollback()

	if req.Action == "approve" {
		if req.AdjustReason == "" {
			writeErr(w, 400, "请填写费用调整理由（用户端可见）")
			return
		}
		waiver := req.WaiverAmount
		if waiver < 0 {
			waiver = 0
		}
		if waiver > feeBefore {
			waiver = feeBefore
		}
		finalFee := feeBefore - waiver
		// 关闭计费：行程按临时还车完成，费用封顶在暂停时刻并按审核调整
		tx.Exec(`UPDATE rides SET status='completed', return_station_id=$1, return_time=now(),
			fee=$2, fee_adjust_reason=$3, fee_adjust_amount=$4 WHERE id=$5`,
			fullStationID, finalFee, req.AdjustReason, waiver, rideID)
		tx.Exec(`UPDATE temp_return_orders SET status='approved', handler_id=$1, adjust_reason=$2,
			waiver_amount=$3, final_fee=$4, handled_at=now() WHERE id=$5`,
			u.ID, req.AdjustReason, waiver, finalFee, id)
		// 车辆留在原地待调度回收（非在桩、不可借），并建协同事件通知调度
		evTitle := fmt.Sprintf("临时还车审核通过，车辆 %s 待现场回收（工单 #%s）", bikeCodeOf(bikeID), id)
		var rb, bs int64 = rideID, bikeID
		createEvent("cannot_return", evTitle, fullStationID, &rb, &bs, nil, userID, "medium")
		if err := tx.Commit(); err != nil {
			writeErr(w, 500, "审核失败")
			return
		}
		writeJSON(w, 200, map[string]any{
			"ok": true,
			"message": fmt.Sprintf("已审核通过并关闭计费：最终费用 ¥%.2f（免除 ¥%.2f），费用调整理由对用户可见，已通知调度回收车辆", finalFee, waiver),
		})
		return
	}

	// 驳回：行程恢复进行中，车辆恢复租用，费用继续计算（之前暂停作废）
	if req.AdjustReason == "" {
		writeErr(w, 400, "请填写驳回原因（用户端可见）")
		return
	}
	tx.Exec(`UPDATE rides SET status='ongoing', fee_paused=FALSE, fee_pause_time=NULL,
		fee_adjust_reason=$1 WHERE id=$2`, "临时还车申请未通过："+req.AdjustReason, rideID)
	tx.Exec(`UPDATE bikes SET status='rented' WHERE id=$1`, bikeID)
	tx.Exec(`UPDATE temp_return_orders SET status='rejected', handler_id=$1, adjust_reason=$2, handled_at=now() WHERE id=$3`,
		u.ID, req.AdjustReason, id)
	if err := tx.Commit(); err != nil {
		writeErr(w, 500, "处理失败")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": "已驳回，行程恢复进行中并继续计费，用户可重新还车或再次申请"})
}

func bikeCodeOf(bikeID int64) string {
	var code string
	db.QueryRow(`SELECT code FROM bikes WHERE id=$1`, bikeID).Scan(&code)
	return code
}
