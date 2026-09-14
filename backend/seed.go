package main

import (
	"database/sql"
	"fmt"
)

// seed inserts demo data on first run (when users table is empty).
func seed() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM users`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	ex := func(q string, args ...any) sql.Result {
		r, err := tx.Exec(q, args...)
		if err != nil {
			panic(fmt.Sprintf("seed failed: %v\nquery: %s", err, q))
		}
		return r
	}
	one := func(q string, args ...any) int64 {
		var id int64
		if err := tx.QueryRow(q, args...).Scan(&id); err != nil {
			panic(fmt.Sprintf("seed failed: %v\nquery: %s", err, q))
		}
		return id
	}

	// ---------- users ----------
	pw := hashPassword("123456")
	uid := map[string]int64{}
	addUser := func(username, name, role string, deposit, balance float64, status string, restricted bool) int64 {
		id := one(`INSERT INTO users(username,password_hash,name,role,deposit,balance,status,restricted)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
			username, pw, name, role, deposit, balance, status, restricted)
		uid[username] = id
		return id
	}
	addUser("rider1", "王小明", "user", 199, 86.5, "active", false)
	addUser("rider2", "李小红", "user", 0, 20, "active", false)   // 押金不足演示
	addUser("rider3", "张小刚", "user", 199, 50, "active", true)  // 骑行限制演示
	addUser("rider4", "赵小芳", "user", 199, 42, "active", false)
	addUser("rider5", "陈小军", "user", 199, 63, "active", false)
	addUser("rider6", "林小梅", "user", 199, 30, "active", false)
	addUser("cs1", "王芳", "cs", 0, 0, "active", false)
	addUser("disp1", "李强", "dispatcher", 0, 0, "active", false)
	addUser("repair1", "赵建国", "repair", 0, 0, "active", false)
	addUser("admin1", "孙丽", "station_admin", 0, 0, "active", false)
	addUser("ops1", "周明", "operator", 0, 0, "active", false)
	addUser("city1", "城管委观察员", "city", 0, 0, "active", false)
	addUser("driver1", "周师傅", "driver", 0, 0, "active", false)
	addUser("driver2", "吴师傅", "driver", 0, 0, "active", false)
	addUser("driver3", "郑师傅", "driver", 0, 0, "active", false)
	// 流失用户（30 天前最后骑行，用于站点调整分析）
	oldUsers := []int64{}
	for i := 1; i <= 8; i++ {
		oldUsers = append(oldUsers, addUser(fmt.Sprintf("olduser%d", i),
			fmt.Sprintf("老用户%d", i), "user", 199, 10, "active", false))
	}

	// ---------- stations ----------
	type st struct {
		code, name, typ string
		cap             int
		x, y            float64
		addr            string
		nearFZ          bool
	}
	stations := []st{
		{"ST01", "地铁文化广场站", "subway", 24, 18, 22, "文化广场地铁站B口", false},
		{"ST02", "地铁东站南口站", "subway", 20, 72, 18, "火车东站南广场", false},
		{"ST03", "实验中学站", "school", 16, 30, 55, "实验中学正门", true},
		{"ST04", "万象城商圈站", "business", 28, 58, 48, "万象城购物中心西门", false},
		{"ST05", "滨江社区站", "residential", 18, 15, 78, "滨江社区北门", true},
		{"ST06", "科技园站", "business", 22, 82, 66, "科技园3号楼", false},
		{"ST07", "中心医院站", "residential", 14, 45, 30, "中心医院急诊楼旁", false},
		{"ST08", "大学城站", "school", 26, 68, 85, "大学城生活区东门", false},
	}
	stID := []int64{}
	for _, s := range stations {
		stID = append(stID, one(`INSERT INTO stations(code,name,type,capacity,location_x,location_y,address,near_forbidden_zone)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
			s.code, s.name, s.typ, s.cap, s.x, s.y, s.addr, s.nearFZ))
	}

	// ---------- bikes & docks ----------
	// 每站健康在桩车辆数（ST04 接近满桩，ST01/ST06 接近空桩）
	// 健康在桩 3+5+4+27+5+2+2+2 = 50；故障在桩 2（ST04、ST02 各1）；
	// 另：租用3、维修中2、调拨在途2、清洗1、报废1 → 共 61 辆
	dockedCounts := []int{3, 5, 4, 27, 5, 2, 2, 2}
	bikeNo := 0
	newBike := func(status string, stationID any) int64 {
		bikeNo++
		id := one(`INSERT INTO bikes(code,status,station_id) VALUES($1,$2,$3) RETURNING id`,
			fmt.Sprintf("BK%04d", bikeNo), status, stationID)
		return id
	}
	for si, s := range stations {
		stationID := stID[si]
		dockIDs := []int64{}
		for d := 1; d <= s.cap; d++ {
			dockIDs = append(dockIDs, one(`INSERT INTO docks(station_id,dock_no,status) VALUES($1,$2,'empty') RETURNING id`, stationID, d))
		}
		for k := 0; k < dockedCounts[si]; k++ {
			bid := newBike("docked", stationID)
			ex(`UPDATE docks SET status='occupied', bike_id=$1 WHERE id=$2`, bid, dockIDs[k])
		}
	}
	// 故障在桩车：ST04 最后 1 个桩放故障车 → 28/28 满桩
	bkFault1 := newBike("fault", stID[3])
	ex(`UPDATE docks SET status='occupied', bike_id=$1 WHERE station_id=$2 AND dock_no=$3`, bkFault1, stID[3], 28)
	// ST02 故障车 1 辆
	bkFault2 := newBike("fault", stID[1])
	ex(`UPDATE docks SET status='occupied', bike_id=$1 WHERE station_id=$2 AND dock_no=$3`, bkFault2, stID[1], 20)
	// 维修中 2 辆
	bkRepair1 := newBike("in_repair", nil)
	bkRepair2 := newBike("in_repair", nil)
	// 调拨在途 2 辆（在调拨车 苏A·D1003 上）
	newBike("in_transit", nil)
	newBike("in_transit", nil)
	// 清洗 1 辆
	bkCleaning := newBike("cleaning", stID[6])
	// 报废 1 辆
	bkScrapped := newBike("scrapped", nil)
	// 租用中 3 辆
	bkRent1 := newBike("rented", nil)
	bkRent2 := newBike("rented", nil)
	bkRent3 := newBike("rented", nil)
	// 重复故障演示车（BK0048 附近编号）：取 ST05 一辆在桩车
	var bkRepeat int64
	err = tx.QueryRow(`SELECT id FROM bikes WHERE station_id=$1 AND status='docked' LIMIT 1`, stID[4]).Scan(&bkRepeat)
	if err != nil {
		return err
	}

	// ---------- rides ----------
	// rider1 被申诉关联的行程（费用争议）
	appealRide := one(`INSERT INTO rides(user_id,bike_id,borrow_station_id,borrow_dock_id,borrow_time,
		return_station_id,return_dock_id,return_time,fee,status,user_location)
		VALUES($1,$2,$3,(SELECT id FROM docks WHERE station_id=$3 AND dock_no=2),now()-interval '26 hours',
		$4,(SELECT id FROM docks WHERE station_id=$4 AND dock_no=5),now()-interval '24 hours',6.00,'completed','万象城西门公交站')
		RETURNING id`, uid["rider1"], 1, stID[0], stID[3])
	_ = appealRide

	// 历史行程（近 3 天，分布于各站）
	riders := []int64{uid["rider1"], uid["rider4"], uid["rider5"], uid["rider6"]}
	for i := 0; i < 26; i++ {
		u := riders[i%len(riders)]
		from := stID[i%len(stID)]
		to := stID[(i+3)%len(stID)]
		fee := 1.5 * float64(1+i%3)
		ex(`INSERT INTO rides(user_id,bike_id,borrow_station_id,borrow_dock_id,borrow_time,
			return_station_id,return_dock_id,return_time,fee,status,user_location)
			VALUES($1,$2,$3,(SELECT id FROM docks WHERE station_id=$3 AND dock_no=1),
			now()-($5||' hours')::interval,
			$4,(SELECT id FROM docks WHERE station_id=$4 AND dock_no=2),
			now()-($5||' hours')::interval + (($6||' minutes')::interval),$7,'completed','站点周边50米')`,
			u, i+1, from, to, 4+i*3, 15+i%35, fee)
	}
	// 流失用户的历史行程（35-60 天前，最后一骑在 ST04 / ST01）
	for i, ou := range oldUsers {
		stIdx := 3
		if i%2 == 0 {
			stIdx = 0
		}
		ex(`INSERT INTO rides(user_id,bike_id,borrow_station_id,borrow_time,return_station_id,return_time,fee,status,user_location)
			VALUES($1,$2,$3,now()-($4||' days')::interval,$5,now()-($4||' days')::interval + interval '25 minutes',1.5,'completed','')`,
			ou, i+1, stID[stIdx], 35+i*3, stID[(stIdx+1)%len(stID)])
	}
	// 进行中行程 3 条
	ex(`INSERT INTO rides(user_id,bike_id,borrow_station_id,borrow_dock_id,borrow_time,status)
		VALUES($1,$2,$3,(SELECT id FROM docks WHERE station_id=$3 AND dock_no=6),now()-interval '25 minutes','ongoing')`,
		uid["rider4"], bkRent1, stID[4])
	ex(`INSERT INTO rides(user_id,bike_id,borrow_station_id,borrow_dock_id,borrow_time,status)
		VALUES($1,$2,$3,(SELECT id FROM docks WHERE station_id=$3 AND dock_no=3),now()-interval '40 minutes','ongoing')`,
		uid["rider5"], bkRent2, stID[2])
	ex(`INSERT INTO rides(user_id,bike_id,borrow_station_id,borrow_dock_id,borrow_time,status)
		VALUES($1,$2,$3,(SELECT id FROM docks WHERE station_id=$3 AND dock_no=1),now()-interval '15 minutes','ongoing')`,
		uid["rider6"], bkRent3, stID[6])

	// ---------- parts ----------
	partIDs := map[string]int64{}
	for _, p := range []struct {
		name  string
		stock int
	}{
		{"刹车线", 25}, {"智能锁芯", 12}, {"链条", 18}, {"轮胎", 20}, {"坐垫", 15}, {"脚踏", 30},
	} {
		partIDs[p.name] = one(`INSERT INTO parts(name,stock) VALUES($1,$2) RETURNING id`, p.name, p.stock)
	}

	// ---------- faults & repairs ----------
	one(`INSERT INTO faults(bike_id,station_id,type,description,status,reporter_id,created_at)
		VALUES($1,$2,'brake','还车时发现刹车偏软，捏把行程过长','pending',$3,now()-interval '3 hours') RETURNING id`,
		bkFault1, stID[3], uid["rider5"])
	one(`INSERT INTO faults(bike_id,station_id,type,description,status,reporter_id,created_at)
		VALUES($1,$2,'lock','车锁指示灯不亮，无法开锁','pending',$3,now()-interval '5 hours') RETURNING id`,
		bkFault2, stID[1], uid["rider6"])
	one(`INSERT INTO faults(bike_id,type,description,status,reporter_id,created_at)
		VALUES($1,'chain','链条脱落，无法踩踏','repairing',$2,now()-interval '1 day') RETURNING id`, bkRepair1, uid["rider4"])
	one(`INSERT INTO faults(bike_id,type,description,status,reporter_id,created_at)
		VALUES($1,'tire','前轮胎压不足，疑似慢撒气','repairing',$2,now()-interval '2 days') RETURNING id`, bkRepair2, uid["rider5"])
	f5 := one(`INSERT INTO faults(bike_id,type,description,status,reporter_id,created_at,resolved_at)
		VALUES($1,'brake','刹车总成严重磨损','scrapped',$2,now()-interval '40 days',now()-interval '38 days') RETURNING id`,
		bkScrapped, uid["rider1"])
	f6 := one(`INSERT INTO faults(bike_id,type,description,status,reporter_id,created_at,resolved_at)
		VALUES($1,'brake','刹车异响','fixed',$2,now()-interval '20 days',now()-interval '20 days'+interval '50 minutes') RETURNING id`,
		bkRepeat, uid["rider4"])
	f7 := one(`INSERT INTO faults(bike_id,type,description,status,reporter_id,created_at,resolved_at)
		VALUES($1,'brake','刹车再次异响，制动力下降','fixed',$2,now()-interval '5 days',now()-interval '5 days'+interval '45 minutes') RETURNING id`,
		bkRepeat, uid["rider5"])

	// 维修档案：故障类型 / 用时 / 配件 / 重复故障 / 报废
	r1 := one(`INSERT INTO repairs(fault_id,bike_id,repairer_id,started_at,finished_at,duration_min,result,notes,is_repeat)
		VALUES($1,$2,$3,now()-interval '20 days',now()-interval '20 days'+interval '35 minutes',35,'fixed','更换刹车线，调试制动行程',false) RETURNING id`,
		f6, bkRepeat, uid["repair1"])
	ex(`INSERT INTO repair_parts(repair_id,part_id,qty) VALUES($1,$2,1)`, r1, partIDs["刹车线"])
	r2 := one(`INSERT INTO repairs(fault_id,bike_id,repairer_id,started_at,finished_at,duration_min,result,notes,is_repeat)
		VALUES($1,$2,$3,now()-interval '5 days',now()-interval '5 days'+interval '40 minutes',40,'fixed','30天内同车型同故障第二次维修，建议重点观察',true) RETURNING id`,
		f7, bkRepeat, uid["repair1"])
	ex(`INSERT INTO repair_parts(repair_id,part_id,qty) VALUES($1,$2,1)`, r2, partIDs["刹车线"])
	r3 := one(`INSERT INTO repairs(fault_id,bike_id,repairer_id,started_at,finished_at,duration_min,result,notes)
		VALUES($1,$2,$3,now()-interval '39 days',now()-interval '38 days',25,'scrapped','刹车总成分解后磨损严重，车架变形，无维修价值，做报废处理') RETURNING id`,
		f5, bkScrapped, uid["repair1"])
	ex(`INSERT INTO repair_parts(repair_id,part_id,qty) VALUES($1,$2,1)`, r3, partIDs["刹车线"])
	ex(`UPDATE parts SET stock=stock-3 WHERE name='刹车线'`)

	// ---------- trucks & shifts ----------
	tr1 := one(`INSERT INTO trucks(plate,capacity,status,driver_id,location_x,location_y)
		VALUES('苏A·D1001',30,'idle',$1,20,25) RETURNING id`, uid["driver1"])
	tr2 := one(`INSERT INTO trucks(plate,capacity,status,driver_id,location_x,location_y)
		VALUES('苏A·D1002',24,'idle',$1,60,50) RETURNING id`, uid["driver2"])
	tr3 := one(`INSERT INTO trucks(plate,capacity,status,driver_id,location_x,location_y)
		VALUES('苏A·D1003',20,'en_route',$1,55,70) RETURNING id`, uid["driver3"])
	ex(`INSERT INTO driver_shifts(driver_id,shift_date,start_time,end_time,truck_id,status)
		VALUES($1,CURRENT_DATE,'06:30','14:30',$2,'active')`, uid["driver1"], tr1)
	ex(`INSERT INTO driver_shifts(driver_id,shift_date,start_time,end_time,truck_id,status)
		VALUES($1,CURRENT_DATE,'14:30','22:30',$2,'scheduled')`, uid["driver2"], tr2)
	ex(`INSERT INTO driver_shifts(driver_id,shift_date,start_time,end_time,truck_id,status)
		VALUES($1,CURRENT_DATE,'06:30','14:30',$2,'active')`, uid["driver3"], tr3)

	// ---------- rebalance tasks ----------
	ex(`INSERT INTO rebalance_tasks(from_station_id,to_station_id,bike_count,truck_id,driver_id,status,reason,explanation,factors,cost,created_by,created_at,completed_at)
		VALUES($1,$2,12,$3,$4,'completed','地铁早高峰补车',
		'地铁早高峰（7-9点）：地铁文化广场站预计借车需求 42 辆，当时在桩仅 5 辆；滨江社区站同期还车集中、在桩 14/18 辆。从滨江社区站调拨 12 辆至地铁文化广场站，调拨车容量 30，单程约 18 分钟。',
		'{"subway_peak":true,"school_dismissal":false,"business_event":false,"repair_ratio":0.05,"truck_capacity":30}',45.60,$5,
		now()-interval '30 hours',now()-interval '29 hours')`,
		stID[4], stID[0], tr1, uid["driver1"], uid["disp1"])
	ex(`INSERT INTO rebalance_tasks(from_station_id,to_station_id,bike_count,truck_id,driver_id,status,reason,explanation,factors,cost,created_by,created_at,completed_at)
		VALUES($1,$2,10,$3,$4,'completed','商圈活动后回流',
		'商圈活动：万象城周末促销结束，晚间还车高峰后满桩率 96%；科技园站晚间加班借车需求上升。从万象城商圈站调拨 10 辆至科技园站，调拨车容量 24。',
		'{"subway_peak":false,"school_dismissal":false,"business_event":true,"repair_ratio":0.04,"truck_capacity":24}',38.00,$5,
		now()-interval '50 hours',now()-interval '49 hours')`,
		stID[3], stID[5], tr2, uid["driver2"], uid["disp1"])
	task3 := one(`INSERT INTO rebalance_tasks(from_station_id,to_station_id,bike_count,truck_id,driver_id,status,reason,explanation,factors,cost,created_by,created_at)
		VALUES($1,$2,8,$3,$4,'in_progress','学校放学补车',
		'学校放学（15:30-17:30）：大学城站放学时段借车需求预计 35 辆，当前在桩 2 辆；科技园站白天在桩富余。调拨 8 辆至大学城站，调拨车容量 20。',
		'{"subway_peak":false,"school_dismissal":true,"business_event":false,"repair_ratio":0.03,"truck_capacity":20}',33.50,$5,now()-interval '40 minutes')
		RETURNING id`, stID[5], stID[7], tr3, uid["driver3"], uid["disp1"])
	ex(`UPDATE trucks SET current_task_id=$1 WHERE id=$2`, task3, tr3)

	// ---------- events ----------
	addParticipant := func(eventID, userID int64, role string) {
		ex(`INSERT INTO event_participants(event_id,user_id,role) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, eventID, userID, role)
	}
	addMsg := func(eventID, senderID int64, content, typ string, ago string) {
		ex(`INSERT INTO event_messages(event_id,sender_id,content,msg_type,created_at)
			VALUES($1,$2,$3,$4,now()-($5||' minutes')::interval)`, eventID, senderID, content, typ, ago)
	}

	// E1 无法还车（满桩）
	e1 := one(`INSERT INTO events(type,title,station_id,status,priority,created_by,created_at)
		VALUES('cannot_return','万象城商圈站满桩，用户无法还车',$1,'processing','high',$2,now()-interval '50 minutes') RETURNING id`,
		stID[3], uid["rider4"])
	addParticipant(e1, uid["rider4"], "user")
	addParticipant(e1, uid["cs1"], "cs")
	addParticipant(e1, uid["disp1"], "dispatcher")
	addParticipant(e1, uid["admin1"], "station_admin")
	addMsg(e1, uid["rider4"], "我到万象城站还车，28 个桩全满了，APP 提示无法还车，我赶着上班！", "message", "50")
	addMsg(e1, uid["cs1"], "已收到，正在为您查询周边可还站点：科技园站（距 1.2km，空桩 20）、中心医院站（距 900m，空桩 12）。同时已通知调度。", "message", "46")
	addMsg(e1, uid["disp1"], "万象城站连续 3 天晚间满桩，已生成调拨计划：今晚 21:00 从万象城调 12 辆至科技园站。", "message", "40")
	addMsg(e1, uid["admin1"], "站点已安排人员现场引导，并向满桩用户发放 1.5 元骑行券补偿。", "action", "35")

	// E2 刹车异常
	e2 := one(`INSERT INTO events(type,title,station_id,bike_id,status,priority,created_by,created_at)
		VALUES('brake_fault','车辆刹车异常安全隐患',$1,$2,'processing','high',$3,now()-interval '3 hours') RETURNING id`,
		stID[3], bkFault1, uid["rider5"])
	addParticipant(e2, uid["rider5"], "user")
	addParticipant(e2, uid["cs1"], "cs")
	addParticipant(e2, uid["repair1"], "repair")
	addMsg(e2, uid["rider5"], "还车前发现这辆车刹车很软，差点没刹住，太危险了。", "message", "180")
	addMsg(e2, uid["cs1"], "非常抱歉给您带来安全隐患，已将该车标记为故障车并锁定，其他用户无法借出。", "message", "170")
	addMsg(e2, uid["repair1"], "已接单，下午到万象城站现场检修，初步判断刹车线老化，备件已带。", "message", "150")

	// E3 潮汐失衡
	e3 := one(`INSERT INTO events(type,title,station_id,status,priority,created_by,created_at)
		VALUES('tidal_imbalance','地铁文化广场站长期潮汐失衡',$1,'open','medium',$2,now()-interval '2 days') RETURNING id`,
		stID[0], uid["disp1"])
	addParticipant(e3, uid["disp1"], "dispatcher")
	addParticipant(e3, uid["ops1"], "operator")
	addParticipant(e3, uid["admin1"], "station_admin")
	addParticipant(e3, uid["city1"], "city")
	addMsg(e3, uid["disp1"], "该站连续 14 天早高峰 8 点前空桩、晚高峰 20 点后满桩，单向调拨成本每周超 300 元，建议评估扩容或错峰调度。", "message", "2800")
	addMsg(e3, uid["ops1"], "已拉取 30 天数据：早高峰缺口均值 18 辆。建议方案：① 增加 6 个桩位 ② 与滨江社区站建立固定早晚对调班次。", "message", "2700")
	addMsg(e3, uid["city1"], "请运营方提交调整方案说明，城管委将评估人行道扩容空间。", "message", "2600")

	// E4 锁具打不开（已解决）
	e4 := one(`INSERT INTO events(type,title,station_id,status,priority,created_by,created_at,resolved_at,resolution)
		VALUES('lock_stuck','车锁无法打开',$1,'resolved','medium',$2,now()-interval '8 hours',now()-interval '7 hours','客服远程开锁成功，维修员现场更换智能锁芯，车辆恢复运营。') RETURNING id`,
		stID[1], uid["rider6"])
	addParticipant(e4, uid["rider6"], "user")
	addParticipant(e4, uid["cs1"], "cs")
	addParticipant(e4, uid["repair1"], "repair")
	addMsg(e4, uid["rider6"], "扫码后车锁一直打不开，赶时间。", "message", "480")
	addMsg(e4, uid["cs1"], "已为您远程开锁，若 10 秒内未弹开请告诉我。本次行程免费。", "message", "475")
	addMsg(e4, uid["repair1"], "现场检查锁芯卡滞，已更换智能锁芯并测试 5 次开合正常。", "action", "430")

	// E5 与申诉#1 关联的历史协同（还车异常 → 客服处理记录，供申诉溯源）
	e5 := one(`INSERT INTO events(type,title,station_id,ride_id,status,priority,created_by,created_at,resolved_at,resolution)
		VALUES('cannot_return','还车桩位识别异常',$1,$2,'resolved','high',$3,now()-interval '24 hours',now()-interval '23 hours','桩位通信模块故障导致结束计费延迟，已停用该桩位并登记检修。') RETURNING id`,
		stID[3], appealRide, uid["rider1"])
	addParticipant(e5, uid["rider1"], "user")
	addParticipant(e5, uid["cs1"], "cs")
	addParticipant(e5, uid["admin1"], "station_admin")
	addMsg(e5, uid["rider1"], "我已还车锁车，但 APP 一直显示计费中。", "message", "1440")
	addMsg(e5, uid["cs1"], "已核实您在万象城站 5 号桩还车，桩位通信延迟导致未结束计费，已为您手动结束行程。", "message", "1430")
	addMsg(e5, uid["admin1"], "5 号桩已停用检修，通信模块更换中。", "action", "1420")

	// ---------- appeals ----------
	ex(`INSERT INTO appeals(ride_id,user_id,reason,status,created_at)
		VALUES($1,$2,'还车时车桩故障未识别成功，APP 显示持续计费 2 小时，多扣费 4.5 元，要求退费并说明责任。','pending',now()-interval '20 hours')`,
		appealRide, uid["rider1"])
	var resolvedRideID int64
	err = tx.QueryRow(`SELECT id FROM rides WHERE user_id=$1 AND status='completed' AND id<>$2 LIMIT 1`, uid["rider4"], appealRide).Scan(&resolvedRideID)
	if err != nil {
		return err
	}
	ex(`INSERT INTO appeals(ride_id,user_id,reason,status,handler_id,resolution,resp_fee,resp_bike,resp_station,refund,created_at,resolved_at)
		VALUES($1,$2,'还车后未收到结束计费通知，被多扣 3 元。','resolved',$3,
		'核实还车桩位通信延迟导致结束计费延迟 1 小时，已退还多收费用 3 元。',
		'平台计费规则（通信延迟）','无责任','桩位通信模块故障，已停用检修',3.00,now()-interval '3 days',now()-interval '2 days')`,
		resolvedRideID, uid["rider4"], uid["cs1"])

	// ---------- complaints ----------
	for i := 0; i < 3; i++ {
		ex(`INSERT INTO complaints(station_id,user_id,type,content,created_at)
			VALUES($1,$2,'full','晚高峰连续无法还车，希望增加桩位或加强调拨',now()-($3||' days')::interval)`,
			stID[3], riders[i], 1+i*2)
	}
	for i := 0; i < 2; i++ {
		ex(`INSERT INTO complaints(station_id,user_id,type,content,created_at)
			VALUES($1,$2,'empty','早高峰 8 点前就没车了，通勤受影响',now()-($3||' days')::interval)`,
			stID[0], riders[i+1], 2+i*3)
	}

	// ---------- forbidden zones / weather ----------
	ex(`INSERT INTO forbidden_zones(name,description,active) VALUES('滨江步道禁骑区','滨江景观步道全线禁止骑行，违停将收取调度费',true)`)
	ex(`INSERT INTO forbidden_zones(name,description,active) VALUES('古城步行街禁骑区','古城步行街 9:00-21:00 禁止骑行',true)`)
	ex(`INSERT INTO weather_alerts(level,content,active,created_at)
		VALUES('橙色','暴雨橙色预警：全市站点暂停借车，还车正常',false,now()-interval '7 days')`)

	// ---------- cleaning ----------
	ex(`INSERT INTO cleaning_schedules(bike_id,station_id,scheduled_date,cleaner,status)
		VALUES($1,$2,CURRENT_DATE,'张保洁','pending')`, bkCleaning, stID[6])
	ex(`INSERT INTO cleaning_schedules(bike_id,station_id,scheduled_date,cleaner,status)
		VALUES((SELECT id FROM bikes WHERE station_id=$1 AND status='docked' LIMIT 1),$1,CURRENT_DATE-1,'张保洁','done')`, stID[1])
	ex(`INSERT INTO cleaning_schedules(bike_id,station_id,scheduled_date,cleaner,status)
		VALUES((SELECT id FROM bikes WHERE station_id=$1 AND status='docked' LIMIT 1),$1,CURRENT_DATE-1,'李保洁','done')`, stID[3])

	// ---------- subway flows (today, hourly) ----------
	subwayStations := []int64{stID[0], stID[1]}
	for _, sid := range subwayStations {
		for h := 0; h < 24; h++ {
			flow := 200
			switch {
			case h >= 7 && h <= 9:
				flow = 2600 + h*120
			case h >= 17 && h <= 19:
				flow = 3000 + (h-17)*200
			case h >= 12 && h <= 13:
				flow = 1200
			case h >= 22 || h <= 5:
				flow = 80
			default:
				flow = 600
			}
			surge := sid == stID[0] && h == 18 // 地铁突发客流
			if surge {
				flow = 5200
			}
			ex(`INSERT INTO subway_flows(station_id,flow_date,hour,flow,surge) VALUES($1,CURRENT_DATE,$2,$3,$4)`,
				sid, h, flow, surge)
		}
	}

	// ---------- demand profiles ----------
	for si, s := range stations {
		for h := 0; h < 24; h++ {
			borrow, ret := 1, 1
			switch s.typ {
			case "subway":
				if h >= 7 && h <= 9 {
					borrow = 14
				}
				if h >= 18 && h <= 20 {
					ret = 16
				}
			case "school":
				if h >= 7 && h <= 8 {
					ret = 10
				}
				if h >= 15 && h <= 17 {
					borrow = 12
				}
			case "business":
				if h >= 11 && h <= 13 {
					ret = 8
				}
				if h >= 18 && h <= 21 {
					ret = 14
				}
				if h >= 13 && h <= 14 {
					borrow = 6
				}
			case "residential":
				if h >= 7 && h <= 9 {
					borrow = 8
				}
				if h >= 18 && h <= 20 {
					ret = 10
				}
			}
			ex(`INSERT INTO demand_profiles(station_id,hour,borrow_need,return_need) VALUES($1,$2,$3,$4)`,
				stID[si], h, borrow, ret)
		}
	}

	// ---------- station adjustments ----------
	ex(`INSERT INTO station_adjustments(station_id,action,detail,operator_id,created_at)
		VALUES($1,'capacity_expand','桩位 22 → 28，应对商圈周末满桩投诉（近 30 天满桩投诉 3 起）',$2,now()-interval '30 days')`,
		stID[3], uid["ops1"])

	return tx.Commit()
}
