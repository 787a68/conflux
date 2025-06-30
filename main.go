package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var Version = "dev"

// 日志级别常量
const (
	INFO  = "INFO"
	WARN  = "WARN"
	ERROR = "ERROR"
)

var (
	logOnce sync.Once
	logFile *os.File
)

// 日志初始化：创建日志文件并设置日志格式
func InitLog(logPath string) error {
	var err error
	logOnce.Do(func() {
		logFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			// 同时输出到控制台和文件
			multiWriter := io.MultiWriter(os.Stdout, logFile)
			log.SetOutput(multiWriter)
			log.SetFlags(log.LstdFlags) // 启用默认日期和时间前缀
		}
	})
	return err
}

// 日志关闭：释放文件句柄
func CloseLog() {
	if logFile != nil {
		_ = logFile.Close()
	}
}

// 日志输出：统一格式，包含级别和模块
func logf(level, module, format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	log.Printf("[%s] [%s] %s", level, module, msg)
}

func Info(module, format string, v ...interface{})  { logf(INFO, module, format, v...) }
func Warn(module, format string, v ...interface{})  { logf(WARN, module, format, v...) }
func Error(module, format string, v ...interface{}) { logf(ERROR, module, format, v...) }

// 获取本周一0点的时间（用于日志文件命名和切割）
func getMondayZero(now time.Time) time.Time {
	offset := (int(now.Weekday()) + 6) % 7 // 周一为0
	monday := now.AddDate(0, 0, -offset)
	return time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, monday.Location())
}

// 清理 n 天前的日志文件
func cleanOldLogs(dir string, days int) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	for _, f := range files {
		if !f.IsDir() {
			if info, err := f.Info(); err == nil && info.ModTime().Before(cutoff) {
				_ = os.Remove(filepath.Join(dir, f.Name()))
			}
		}
	}
}

// 生成 n 位随机小写数字 token
func genToken(n int) string {
	b := make([]byte, n/2)
	_, _ = rand.Read(b)
	return strings.ToLower(hex.EncodeToString(b))[:n]
}

// TOKEN 管理：优先级为 环境变量 > 文件 > 自动生成
// 返回最终 TOKEN 字符串
func getToken(tokenPath string) string {
	token := os.Getenv("TOKEN")
	if token != "" {
		Info("TOKEN", "TOKEN 来源于环境变量")
		// 不写入 token 文件
		return token
	}
	if data, err := os.ReadFile(tokenPath); err == nil {
		token = strings.TrimSpace(string(data))
		Info("TOKEN", "TOKEN 来源于本地文件")
		return token
	}
	token = genToken(32)
	if err := os.WriteFile(tokenPath, []byte(token), 0644); err != nil {
		Error("TOKEN", "写入 token 文件失败: %v", err)
	} else {
		Info("TOKEN", "自动生成并写入 TOKEN: %s", token)
	}
	return token
}

// 节点配置文件检查与自动更新
func checkAndUpdateNodeConf(nodeConf string) {
	if _, err := os.Stat(nodeConf); os.IsNotExist(err) {
		Warn("CONF", "未检测到 node.conf，自动执行 update")
		updateNodes()
	}
}

// 定时任务：每隔6小时检查 node.conf 是否超时未更新
func startNodeConfChecker(nodeConf string) {
	go func() {
		for {
			time.Sleep(6 * time.Hour)
			if info, err := os.Stat(nodeConf); err == nil {
				if time.Since(info.ModTime()) > 24*time.Hour {
					Warn("CONF", "node.conf 超过 24 小时未更新，自动执行 update")
					updateNodes()
				}
			}
		}
	}()
}

// 日志文件自动切换：每到周一切换新日志文件
func startLogRotator(logDir string, monday *time.Time) {
	go func() {
		for {
			time.Sleep(time.Minute)
			now := time.Now()
			newMonday := getMondayZero(now)
			if newMonday.After(*monday) {
				*monday = newMonday
				newLogFile := filepath.Join(logDir, monday.Format("2006-01-02-15-04-05")+".log")
				CloseLog()
				if err := InitLog(newLogFile); err == nil {
					Info("SYS", "周一自动切换日志文件: %s", newLogFile)
				}
			}
		}
	}()
}

// 主入口：初始化各模块并启动服务
func main() {
	// 设置全局本地时间为东八区（北京时间）
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err == nil {
		time.Local = loc
	}

	// 1. 日志系统初始化（提前初始化，保证所有日志都规范输出）
	logDir := "/data/conflux/log"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		fmt.Printf("[ERROR] 创建日志目录失败: %v\n", err)
		os.Exit(1)
	}
	now := time.Now()
	monday := getMondayZero(now)
	// 使用当前时间命名初始日志文件
	logFile := filepath.Join(logDir, now.Format("2006-01-02-15-04-05")+".log")
	if err := InitLog(logFile); err != nil {
		fmt.Printf("[ERROR] 日志文件创建失败: %v\n", err)
		os.Exit(1)
	}
	defer CloseLog()
	Info("SYS", "版本号: %s", Version)
	Info("SYS", "工作目录: %s", getCurrentDir())
	Info("SYS", "日志目录: %s", logDir)
	cleanOldLogs(logDir, 7)
	startLogRotator(logDir, &monday)

	// 2. TOKEN 管理
	tokenPath := "/data/conflux/token"
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0755); err != nil {
		Error("SYS", "创建 token 目录失败: %v", err)
	} else {
		Info("SYS", "Token 目录创建成功: %s", filepath.Dir(tokenPath))
	}
	_ = getToken(tokenPath)

	// 3. 节点配置文件检查与自动更新
	nodeConf := "/data/conflux/node.conf"
	if err := os.MkdirAll(filepath.Dir(nodeConf), 0755); err != nil {
		Error("SYS", "创建 node.conf 目录失败: %v", err)
	} else {
		Info("SYS", "Node.conf 目录创建成功: %s", filepath.Dir(nodeConf))
	}
	checkAndUpdateNodeConf(nodeConf)

	// 4. 定时任务：每隔6小时检查 node.conf 是否超时未更新
	startNodeConfChecker(nodeConf)

	// 5. 启动 HTTP 服务
	Info("HTTP", "启动 HTTP 服务...")
	startServer()
}

// 获取当前工作目录
func getCurrentDir() string {
	if dir, err := os.Getwd(); err == nil {
		return dir
	}
	return "unknown"
}
