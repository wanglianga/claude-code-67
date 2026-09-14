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
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
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
`
