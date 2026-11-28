package main

import (
	"fmt"
	"log"
	"os"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"uvplatform.com/uvp-gb28181/app/gb28181/models"
)

func main() {
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		dsn = "root:root@tcp(127.0.0.1:3306)/uvp_gb28181?charset=utf8mb4&parseTime=True&loc=Local"
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// AutoMigrate new table
	if err := db.AutoMigrate(&models.GbFirmwareRepository{}); err != nil {
		log.Fatalf("Failed to migrate GbFirmwareRepository: %v", err)
	}
	fmt.Println("✓ Created gb_firmware_repository table")

	// Add firmware_id column to gb_device_firmware_upgrade
	if err := db.Migrator().AddColumn(&models.GbDeviceFirmwareUpgrade{}, "FirmwareID"); err != nil {
		// Ignore if column already exists
		if !db.Migrator().HasColumn(&models.GbDeviceFirmwareUpgrade{}, "firmware_id") {
			log.Fatalf("Failed to add firmware_id column: %v", err)
		}
	}
	fmt.Println("✓ Added firmware_id column to gb_device_firmware_upgrade")

	fmt.Println("\nNext steps:")
	fmt.Println("  cd resource/database/baseline")
	fmt.Println("  python3 capture_schema.py")
	fmt.Println("  python3 generate_sql.py")
	fmt.Println("  git diff ../uvp-gb28181.sql ../postgresql_converted.sql ../sqlserver_converted.sql")
}
