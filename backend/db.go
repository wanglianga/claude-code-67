package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/lib/pq"
)

var db *sql.DB

func initDB() {
	host := getenv("DB_HOST", "db")
	port := getenv("DB_PORT", "5432")
	user := getenv("DB_USER", "bike")
	pass := getenv("DB_PASSWORD", "bike123")
	name := getenv("DB_NAME", "bikedb")
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable timezone=Asia/Shanghai",
		host, port, user, pass, name)

	var err error
	db, err = sql.Open("postgres", dsn)
	if err != nil {
		log.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		if err = db.Ping(); err == nil {
			break
		}
		log.Printf("waiting for database... (%d)", i+1)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Fatalf("cannot connect to database: %v", err)
	}
	if err := migrate(); err != nil {
		log.Fatalf("migrate failed: %v", err)
	}
	if err := seed(); err != nil {
		log.Fatalf("seed failed: %v", err)
	}
	log.Println("database ready")
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func migrate() error {
	_, err := db.Exec(schemaSQL)
	return err
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS users (
  id SERIAL PRIMARY KEY,
  username TEXT UNIQUE NOT NULL,
  password_hash TEXT NOT NULL,
  name TEXT NOT NULL,
  role TEXT NOT NULL,
  deposit NUMERIC(10,2) NOT NULL DEFAULT 0,
  balance NUMERIC(10,2) NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'active',
  restricted BOOLEAN NOT NULL DEFAULT FALSE,
  phone TEXT DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS sessions (
  token TEXT PRIMARY KEY,
  user_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS stations (
  id SERIAL PRIMARY KEY,
  code TEXT UNIQUE NOT NULL,
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  capacity INT NOT NULL,
  location_x REAL NOT NULL DEFAULT 50,
  location_y REAL NOT NULL DEFAULT 50,
  address TEXT DEFAULT '',
  power_status TEXT NOT NULL DEFAULT 'normal',
  status TEXT NOT NULL DEFAULT 'normal',
  near_forbidden_zone BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS bikes (
  id SERIAL PRIMARY KEY,
  code TEXT UNIQUE NOT NULL,
  status TEXT NOT NULL DEFAULT 'docked',
  station_id INT REFERENCES stations(id),
  lock_status TEXT NOT NULL DEFAULT 'locked',
  total_rides INT NOT NULL DEFAULT 0,
  last_cleaned_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS docks (
  id SERIAL PRIMARY KEY,
  station_id INT NOT NULL REFERENCES stations(id) ON DELETE CASCADE,
  dock_no INT NOT NULL,
  status TEXT NOT NULL DEFAULT 'empty',
  bike_id INT REFERENCES bikes(id),
  UNIQUE(station_id, dock_no)
);

CREATE TABLE IF NOT EXISTS rides (
  id SERIAL PRIMARY KEY,
  user_id INT NOT NULL REFERENCES users(id),
  bike_id INT NOT NULL REFERENCES bikes(id),
  borrow_station_id INT NOT NULL REFERENCES stations(id),
  borrow_dock_id INT REFERENCES docks(id),
  borrow_time TIMESTAMPTZ NOT NULL DEFAULT now(),
  return_station_id INT REFERENCES stations(id),
  return_dock_id INT REFERENCES docks(id),
  return_time TIMESTAMPTZ,
  fee NUMERIC(10,2),
  status TEXT NOT NULL DEFAULT 'ongoing',
  user_location TEXT DEFAULT '',
  fault_type TEXT DEFAULT '',
  fault_feedback TEXT DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS faults (
  id SERIAL PRIMARY KEY,
  bike_id INT NOT NULL REFERENCES bikes(id),
  ride_id INT REFERENCES rides(id),
  station_id INT REFERENCES stations(id),
  type TEXT NOT NULL,
  description TEXT DEFAULT '',
  status TEXT NOT NULL DEFAULT 'pending',
  reporter_id INT REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  resolved_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS repairs (
  id SERIAL PRIMARY KEY,
  fault_id INT NOT NULL REFERENCES faults(id),
  bike_id INT NOT NULL REFERENCES bikes(id),
  repairer_id INT NOT NULL REFERENCES users(id),
  started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  finished_at TIMESTAMPTZ,
  duration_min INT,
  result TEXT,
  notes TEXT DEFAULT '',
  is_repeat BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS parts (
  id SERIAL PRIMARY KEY,
  name TEXT UNIQUE NOT NULL,
  stock INT NOT NULL DEFAULT 0,
  unit TEXT NOT NULL DEFAULT '件'
);

CREATE TABLE IF NOT EXISTS repair_parts (
  id SERIAL PRIMARY KEY,
  repair_id INT NOT NULL REFERENCES repairs(id) ON DELETE CASCADE,
  part_id INT NOT NULL REFERENCES parts(id),
  qty INT NOT NULL
);

CREATE TABLE IF NOT EXISTS trucks (
  id SERIAL PRIMARY KEY,
  plate TEXT UNIQUE NOT NULL,
  capacity INT NOT NULL,
  status TEXT NOT NULL DEFAULT 'idle',
  driver_id INT REFERENCES users(id),
  location_x REAL NOT NULL DEFAULT 50,
  location_y REAL NOT NULL DEFAULT 50,
  current_task_id INT
);

CREATE TABLE IF NOT EXISTS driver_shifts (
  id SERIAL PRIMARY KEY,
  driver_id INT NOT NULL REFERENCES users(id),
  shift_date DATE NOT NULL,
  start_time TEXT NOT NULL,
  end_time TEXT NOT NULL,
  truck_id INT REFERENCES trucks(id),
  status TEXT NOT NULL DEFAULT 'scheduled'
);

CREATE TABLE IF NOT EXISTS rebalance_tasks (
  id SERIAL PRIMARY KEY,
  from_station_id INT NOT NULL REFERENCES stations(id),
  to_station_id INT NOT NULL REFERENCES stations(id),
  bike_count INT NOT NULL,
  truck_id INT REFERENCES trucks(id),
  driver_id INT REFERENCES users(id),
  status TEXT NOT NULL DEFAULT 'pending',
  reason TEXT DEFAULT '',
  explanation TEXT DEFAULT '',
  factors TEXT DEFAULT '{}',
  cost NUMERIC(10,2) DEFAULT 0,
  created_by INT REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  completed_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS events (
  id SERIAL PRIMARY KEY,
  type TEXT NOT NULL,
  title TEXT NOT NULL,
  station_id INT REFERENCES stations(id),
  ride_id INT REFERENCES rides(id),
  bike_id INT REFERENCES bikes(id),
  task_id INT REFERENCES rebalance_tasks(id),
  status TEXT NOT NULL DEFAULT 'open',
  priority TEXT NOT NULL DEFAULT 'medium',
  created_by INT REFERENCES users(id),
  resolution TEXT DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  resolved_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS event_participants (
  id SERIAL PRIMARY KEY,
  event_id INT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  user_id INT NOT NULL REFERENCES users(id),
  role TEXT NOT NULL,
  joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(event_id, user_id)
);

CREATE TABLE IF NOT EXISTS event_messages (
  id SERIAL PRIMARY KEY,
  event_id INT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  sender_id INT REFERENCES users(id),
  content TEXT NOT NULL,
  msg_type TEXT NOT NULL DEFAULT 'message',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS appeals (
  id SERIAL PRIMARY KEY,
  ride_id INT NOT NULL REFERENCES rides(id),
  user_id INT NOT NULL REFERENCES users(id),
  reason TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  handler_id INT REFERENCES users(id),
  resolution TEXT DEFAULT '',
  resp_fee TEXT DEFAULT '',
  resp_bike TEXT DEFAULT '',
  resp_station TEXT DEFAULT '',
  refund NUMERIC(10,2) DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  resolved_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS complaints (
  id SERIAL PRIMARY KEY,
  station_id INT NOT NULL REFERENCES stations(id),
  user_id INT REFERENCES users(id),
  type TEXT NOT NULL,
  content TEXT DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS forbidden_zones (
  id SERIAL PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT DEFAULT '',
  active BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE IF NOT EXISTS weather_alerts (
  id SERIAL PRIMARY KEY,
  level TEXT NOT NULL,
  content TEXT NOT NULL,
  active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS cleaning_schedules (
  id SERIAL PRIMARY KEY,
  bike_id INT NOT NULL REFERENCES bikes(id),
  station_id INT REFERENCES stations(id),
  scheduled_date DATE NOT NULL,
  cleaner TEXT DEFAULT '',
  status TEXT NOT NULL DEFAULT 'pending'
);

CREATE TABLE IF NOT EXISTS subway_flows (
  id SERIAL PRIMARY KEY,
  station_id INT NOT NULL REFERENCES stations(id),
  flow_date DATE NOT NULL DEFAULT CURRENT_DATE,
  hour INT NOT NULL,
  flow INT NOT NULL DEFAULT 0,
  surge BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE IF NOT EXISTS demand_profiles (
  id SERIAL PRIMARY KEY,
  station_id INT NOT NULL REFERENCES stations(id),
  hour INT NOT NULL,
  borrow_need INT NOT NULL DEFAULT 0,
  return_need INT NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS station_adjustments (
  id SERIAL PRIMARY KEY,
  station_id INT NOT NULL REFERENCES stations(id),
  action TEXT NOT NULL,
  detail TEXT DEFAULT '',
  operator_id INT REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ========== 早高峰调拨路线 ==========
-- 一条路线 = 1 辆调拨车 + 有序多站点（住宅区装车 → 地铁口/缺口站卸车）。
CREATE TABLE IF NOT EXISTS peak_routes (
  id SERIAL PRIMARY KEY,
  code TEXT UNIQUE NOT NULL,
  name TEXT NOT NULL,
  peak_type TEXT NOT NULL DEFAULT 'morning',      -- morning / evening
  plan_date DATE NOT NULL DEFAULT CURRENT_DATE,
  scheduled_start TIMESTAMPTZ NOT NULL DEFAULT now(),
  status TEXT NOT NULL DEFAULT 'planned',          -- planned/assigned/executing/completed/cancelled
  truck_id INT REFERENCES trucks(id),
  driver_id INT REFERENCES users(id),
  capacity INT NOT NULL DEFAULT 0,                 -- 生成时锁定的调拨车容量
  total_load INT NOT NULL DEFAULT 0,               -- 计划装车总数（受容量约束）
  subway_borrow_need INT NOT NULL DEFAULT 0,       -- 生成依据：地铁口取车需求
  residential_backlog INT NOT NULL DEFAULT 0,      -- 生成依据：住宅区还车积压
  onboard INT NOT NULL DEFAULT 0,                  -- 执行实时：车上当前车辆数
  current_seq INT NOT NULL DEFAULT 0,              -- 执行实时：当前应执行站点序号
  explanation TEXT DEFAULT '',
  factors TEXT DEFAULT '{}',
  created_by INT REFERENCES users(id),
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 路线站点（有序）：pickup=住宅区装车点，dropoff=地铁口/缺口站卸车点。
CREATE TABLE IF NOT EXISTS route_stops (
  id SERIAL PRIMARY KEY,
  route_id INT NOT NULL REFERENCES peak_routes(id) ON DELETE CASCADE,
  seq INT NOT NULL,
  station_id INT NOT NULL REFERENCES stations(id),
  kind TEXT NOT NULL DEFAULT 'dropoff',            -- pickup / dropoff
  planned_load INT NOT NULL DEFAULT 0,             -- 计划装(+)卸(-)数量
  actual_load INT NOT NULL DEFAULT 0,              -- 实际装卸数量
  eta TIMESTAMPTZ,                                  -- 计划预计到达
  actual_arrival TIMESTAMPTZ,                       -- 实际到达（实时更新）
  status TEXT NOT NULL DEFAULT 'pending',          -- pending/arrived/done/skipped
  -- 偏离：未按计划到达/执行
  deviated BOOLEAN NOT NULL DEFAULT FALSE,
  deviation_type TEXT DEFAULT '',                  -- late / early / skipped / short
  deviation_reason TEXT DEFAULT '',                -- 调度员补充的原因（可免考核）
  exemption BOOLEAN NOT NULL DEFAULT FALSE,        -- 道路拥堵等客观原因 → 司机免考核
  impact TEXT DEFAULT '',                           -- 对后续站点补车的影响说明
  UNIQUE(route_id, seq)
);

-- 路线上具体车辆：装车时建立、卸车时更新，支撑“车辆状态实时更新”。
CREATE TABLE IF NOT EXISTS route_bikes (
  id SERIAL PRIMARY KEY,
  route_id INT NOT NULL REFERENCES peak_routes(id) ON DELETE CASCADE,
  bike_id INT NOT NULL REFERENCES bikes(id),
  loaded_stop_id INT REFERENCES route_stops(id),
  unloaded_stop_id INT REFERENCES route_stops(id),
  status TEXT NOT NULL DEFAULT 'onboard'           -- onboard / unloaded
);

-- 高峰天气：路线复盘关联（按日期 + 早/晚高峰）。
CREATE TABLE IF NOT EXISTS peak_weather (
  id SERIAL PRIMARY KEY,
  peak_date DATE NOT NULL,
  peak_type TEXT NOT NULL DEFAULT 'morning',
  condition TEXT NOT NULL DEFAULT '晴',
  temp_c REAL NOT NULL DEFAULT 20,
  wind_level TEXT DEFAULT '',
  alert_level TEXT DEFAULT '',                     -- 无 / 蓝色 / 黄色 / 橙色 / 红色
  summary TEXT DEFAULT '',
  UNIQUE(peak_date, peak_type)
);

-- 调拨复盘：汇总偏离/原因/影响/改进建议，并关联高峰天气。
CREATE TABLE IF NOT EXISTS route_reviews (
  id SERIAL PRIMARY KEY,
  route_id INT NOT NULL REFERENCES peak_routes(id) ON DELETE CASCADE,
  weather_id INT REFERENCES peak_weather(id),
  on_time_rate REAL NOT NULL DEFAULT 0,            -- 准点率
  deviation_count INT NOT NULL DEFAULT 0,
  exempt_count INT NOT NULL DEFAULT 0,             -- 拥堵豁免次数（不计司机考核）
  driver_assessment TEXT DEFAULT 'normal',         -- normal / exempt / accountable
  planned_total INT NOT NULL DEFAULT 0,
  actual_total INT NOT NULL DEFAULT 0,
  shortage INT NOT NULL DEFAULT 0,                 -- 少补车辆（影响后续站点）
  causes TEXT DEFAULT '[]',                        -- 偏离原因归类（进入复盘，优化下次路线）
  improvement TEXT DEFAULT '',
  reviewer_id INT REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ========== 重复故障报废评估 / 车辆资产台账 ==========
-- 既有表补充字段（幂等）：车辆里程与限制投放、行程里程、配件单价
ALTER TABLE bikes  ADD COLUMN IF NOT EXISTS mileage_km NUMERIC(10,1) NOT NULL DEFAULT 0;
ALTER TABLE bikes  ADD COLUMN IF NOT EXISTS deploy_restricted BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE rides  ADD COLUMN IF NOT EXISTS distance_km NUMERIC(6,2);
ALTER TABLE parts  ADD COLUMN IF NOT EXISTS unit_price NUMERIC(10,2) NOT NULL DEFAULT 0;

-- 重复故障报废评估单：汇总维修记录、骑行里程、配件成本，由维修主管裁决。
CREATE TABLE IF NOT EXISTS scrap_assessments (
  id SERIAL PRIMARY KEY,
  bike_id INT NOT NULL REFERENCES bikes(id),
  trigger_fault_id INT REFERENCES faults(id),
  -- 评估快照
  brake_count INT NOT NULL DEFAULT 0,              -- 刹车问题次数
  lock_count INT NOT NULL DEFAULT 0,               -- 车锁问题次数
  tire_count INT NOT NULL DEFAULT 0,               -- 轮胎问题次数
  repeat_count INT NOT NULL DEFAULT 0,             -- 30 天内同车同类型重复次数
  repair_count INT NOT NULL DEFAULT 0,             -- 历史维修次数
  mileage_km NUMERIC(10,1) NOT NULL DEFAULT 0,     -- 累计骑行里程
  parts_cost NUMERIC(10,2) NOT NULL DEFAULT 0,     -- 累计配件成本
  labor_cost NUMERIC(10,2) NOT NULL DEFAULT 0,     -- 累计维修工时成本
  total_cost NUMERIC(10,2) NOT NULL DEFAULT 0,
  recommendation TEXT NOT NULL DEFAULT 'continue', -- continue / restrict / scrap（系统建议）
  decision TEXT NOT NULL DEFAULT 'pending',        -- pending / continue / restrict / scrap
  decision_reason TEXT DEFAULT '',
  decided_by INT REFERENCES users(id),
  decided_at TIMESTAMPTZ,
  procurement_id INT,                              -- 报废后生成的采购计划行
  status TEXT NOT NULL DEFAULT 'open',             -- open / decided
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 车辆资产台账：记录每辆车的资产状态变更（在役/限制投放/报废/残值）。
CREATE TABLE IF NOT EXISTS vehicle_assets (
  id SERIAL PRIMARY KEY,
  bike_id INT NOT NULL REFERENCES bikes(id),
  asset_code TEXT NOT NULL DEFAULT '',             -- 资产编号
  status TEXT NOT NULL DEFAULT 'active',           -- active / restricted / scrapped
  purchase_date DATE NOT NULL DEFAULT CURRENT_DATE,
  purchase_price NUMERIC(10,2) NOT NULL DEFAULT 380,
  salvage_value NUMERIC(10,2) NOT NULL DEFAULT 0,
  accum_parts_cost NUMERIC(10,2) NOT NULL DEFAULT 0,
  accum_repair_cost NUMERIC(10,2) NOT NULL DEFAULT 0,
  mileage_km NUMERIC(10,1) NOT NULL DEFAULT 0,
  last_assessment_id INT,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 采购计划：报废决定同步生成补货需求。
CREATE TABLE IF NOT EXISTS procurement_plan (
  id SERIAL PRIMARY KEY,
  bike_code TEXT DEFAULT '',
  reason TEXT DEFAULT '',
  source_assessment_id INT,
  qty INT NOT NULL DEFAULT 1,
  status TEXT NOT NULL DEFAULT 'planned',          -- planned / ordered / received
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 资产状态复核：报废车辆若仍在站点（账面报废、现场有车）触发，通知维修仓。
CREATE TABLE IF NOT EXISTS asset_reviews (
  id SERIAL PRIMARY KEY,
  bike_id INT NOT NULL REFERENCES bikes(id),
  bike_code TEXT NOT NULL DEFAULT '',
  station_id INT REFERENCES stations(id),
  dock_id INT,
  type TEXT NOT NULL DEFAULT 'scrapped_on_site',   -- scrapped_on_site / ledger_mismatch
  detail TEXT DEFAULT '',
  status TEXT NOT NULL DEFAULT 'open',             -- open / acknowledged / resolved
  notified_warehouse BOOLEAN NOT NULL DEFAULT FALSE,
  warehouse_note TEXT DEFAULT '',
  handler_id INT REFERENCES users(id),
  source_assessment_id INT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  handled_at TIMESTAMPTZ
);

-- ========== 满桩还车引导 / 临时还车处理单 ==========
ALTER TABLE users ADD COLUMN IF NOT EXISTS credit_score INT NOT NULL DEFAULT 100; -- 用户信用分
ALTER TABLE rides ADD COLUMN IF NOT EXISTS fee_paused BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE rides ADD COLUMN IF NOT EXISTS fee_pause_time TIMESTAMPTZ;             -- 接受引导、费用暂停时刻
ALTER TABLE rides ADD COLUMN IF NOT EXISTS guidance_station_id INT REFERENCES stations(id);
ALTER TABLE rides ADD COLUMN IF NOT EXISTS fee_adjust_reason TEXT DEFAULT '';      -- 临时单关闭后的费用调整理由（用户可见）
ALTER TABLE rides ADD COLUMN IF NOT EXISTS fee_adjust_amount NUMERIC(10,2) NOT NULL DEFAULT 0;

-- 附近无空桩时由用户发起、客服审核的临时还车处理单（必须绑定含站点编号的车辆照片与用户位置）。
CREATE TABLE IF NOT EXISTS temp_return_orders (
  id SERIAL PRIMARY KEY,
  ride_id INT NOT NULL REFERENCES rides(id),
  bike_id INT NOT NULL REFERENCES bikes(id),
  user_id INT NOT NULL REFERENCES users(id),
  full_station_id INT REFERENCES stations(id),   -- 满桩、无法还车的站点
  station_code TEXT NOT NULL DEFAULT '',         -- 照片中识别/用户确认的站点编号
  photo_data TEXT NOT NULL DEFAULT '',           -- 车辆+站点编号照片（data URL 演示）
  user_location TEXT DEFAULT '',
  status TEXT NOT NULL DEFAULT 'pending',        -- pending / approved / rejected
  overtime BOOLEAN NOT NULL DEFAULT FALSE,       -- 提交时是否已超时
  fee_paused BOOLEAN NOT NULL DEFAULT FALSE,     -- 该单是否已暂停计费
  pause_time TIMESTAMPTZ,
  fee_before_pause NUMERIC(10,2) NOT NULL DEFAULT 0, -- 暂停时刻已产生费用
  handler_id INT REFERENCES users(id),
  adjust_reason TEXT DEFAULT '',                 -- 客服审核填写的费用调整理由
  waiver_amount NUMERIC(10,2) NOT NULL DEFAULT 0,-- 免除/调整金额
  final_fee NUMERIC(10,2) NOT NULL DEFAULT 0,
  handled_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
`
