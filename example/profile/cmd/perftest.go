package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/pprof"
	"time"

	"github.com/dep2p/pointsub/example/profile/perflib"
)

// 测试类型常量
const (
	TestAll         = "all"         // 运行所有测试
	TestThroughput  = "throughput"  // 运行吞吐量测试
	TestLatency     = "latency"     // 运行延迟测试
	TestConcurrency = "concurrency" // 运行并发测试
	TestStability   = "stability"   // 运行稳定性测试
	TestDataFlow    = "dataflow"    // 运行大数据流测试
)

// 命令行参数
var (
	testType      = flag.String("type", TestAll, "测试类型: all, throughput, latency, concurrency, stability, dataflow")
	outputDir     = flag.String("output", "results", "测试结果输出目录")
	cpuProfile    = flag.String("cpuprofile", "", "记录CPU profile到指定文件")
	memProfile    = flag.String("memprofile", "", "记录内存profile到指定文件")
	verbose       = flag.Bool("verbose", false, "是否输出详细日志")
	limitDuration = flag.Duration("limit", 0, "限制总测试时间 (例如 30m)")
)

func main() {
	flag.Parse()

	// 创建输出目录
	err := os.MkdirAll(*outputDir, 0755)
	if err != nil {
		log.Fatalf("创建输出目录失败: %v", err)
	}

	// 若提供了CPU profile参数，开始收集
	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err != nil {
			log.Fatalf("无法创建CPU profile文件: %v", err)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatalf("无法启动CPU profile: %v", err)
		}
		defer pprof.StopCPUProfile()
	}

	// 打印测试开始信息
	fmt.Printf("======= PointSub性能测试 =======\n")
	fmt.Printf("测试类型: %s\n", *testType)
	fmt.Printf("输出目录: %s\n", *outputDir)
	fmt.Printf("详细模式: %v\n", *verbose)
	fmt.Println("===============================")

	// 创建CSV文件以保存结果
	resultsFilePath := filepath.Join(*outputDir, fmt.Sprintf("results_%s.csv", time.Now().Format("20060102_150405")))
	resultsFile, err := os.Create(resultsFilePath)
	if err != nil {
		log.Fatalf("无法创建结果文件: %v", err)
	}
	defer resultsFile.Close()

	// 写入CSV头
	resultsFile.WriteString("测试名称,消息数,字节数,持续时间(秒),平均吞吐量(MB/s),平均延迟(ms),最小延迟(ms),最大延迟(ms),P50延迟(ms),P90延迟(ms),P99延迟(ms),内存使用(MB),CPU使用(%),错误数,重试数\n")

	// 保存所有测试结果
	var allResults []perflib.TestResult

	// 设置测试开始时间
	testStartTime := time.Now()

	// 根据测试类型运行相应测试
	switch *testType {
	case TestAll:
		// 运行所有测试
		fmt.Println("运行所有性能测试...")
		allResults = append(allResults, perflib.RunAllThroughputTests(*verbose)...)
		allResults = append(allResults, perflib.RunAllLatencyTests(*verbose)...)
		allResults = append(allResults, perflib.RunAllConcurrencyTests(*verbose)...)
		allResults = append(allResults, perflib.RunAllStabilityTests(*verbose)...)
		allResults = append(allResults, perflib.RunAllDataFlowTests(*verbose)...)

	case TestThroughput:
		// 只运行吞吐量测试
		fmt.Println("运行吞吐量测试...")
		allResults = append(allResults, perflib.RunAllThroughputTests(*verbose)...)

	case TestLatency:
		// 只运行延迟测试
		fmt.Println("运行延迟测试...")
		allResults = append(allResults, perflib.RunAllLatencyTests(*verbose)...)

	case TestConcurrency:
		// 只运行并发测试
		fmt.Println("运行并发测试...")
		allResults = append(allResults, perflib.RunAllConcurrencyTests(*verbose)...)

	case TestStability:
		// 只运行稳定性测试
		fmt.Println("运行稳定性测试...")
		allResults = append(allResults, perflib.RunAllStabilityTests(*verbose)...)

	case TestDataFlow:
		// 只运行大数据流测试
		fmt.Println("运行大数据流测试...")
		allResults = append(allResults, perflib.RunAllDataFlowTests(*verbose)...)

	default:
		log.Fatalf("未知的测试类型: %s", *testType)
	}

	// 保存结果到CSV
	for _, result := range allResults {
		// 转换为CSV行
		csvLine := fmt.Sprintf("%s,%d,%d,%.2f,%.2f,%.2f,%.2f,%.2f,%.2f,%.2f,%.2f,%.2f,%.2f,%d,%d\n",
			result.Name,
			result.TotalMessages,
			result.TotalBytes,
			result.Duration.Seconds(),
			result.MBPerSecond,
			result.AvgLatency,
			result.MinLatency,
			result.MaxLatency,
			result.P50Latency,
			result.P90Latency,
			result.P99Latency,
			float64(result.AvgMemoryUsage)/(1024*1024),
			result.AvgCPUUsage,
			result.ErrorCount,
			result.RetryCount)

		resultsFile.WriteString(csvLine)
	}

	// 记录内存profile
	if *memProfile != "" {
		f, err := os.Create(*memProfile)
		if err != nil {
			log.Fatalf("无法创建内存profile文件: %v", err)
		}
		defer f.Close()
		if err := pprof.WriteHeapProfile(f); err != nil {
			log.Fatalf("无法写入内存profile: %v", err)
		}
	}

	// 打印测试总结
	fmt.Printf("\n======= 测试完成 =======\n")
	fmt.Printf("总测试时间: %s\n", time.Since(testStartTime).Round(time.Second))
	fmt.Printf("测试数量: %d\n", len(allResults))
	fmt.Printf("结果保存至: %s\n\n", resultsFilePath)

	// 打印最好和最差结果
	if len(allResults) > 0 {
		// 吞吐量最高的测试
		var bestThroughput perflib.TestResult
		var worstLatency perflib.TestResult

		for i, result := range allResults {
			if i == 0 {
				bestThroughput = result
				worstLatency = result
				continue
			}

			if result.MBPerSecond > bestThroughput.MBPerSecond {
				bestThroughput = result
			}

			if result.P99Latency > worstLatency.P99Latency {
				worstLatency = result
			}
		}

		fmt.Printf("最高吞吐量: %.2f MB/s (%s)\n", bestThroughput.MBPerSecond, bestThroughput.Name)
		fmt.Printf("最高P99延迟: %.2f ms (%s)\n", worstLatency.P99Latency, worstLatency.Name)

		// 计算总传输数据量
		var totalBytes int64
		var totalMsgs int64
		var totalErrors int64
		for _, result := range allResults {
			totalBytes += result.TotalBytes
			totalMsgs += result.TotalMessages
			totalErrors += result.ErrorCount
		}

		// 根据数据大小选择合适的单位
		dataSize := float64(totalBytes)
		var sizeStr string
		switch {
		case dataSize >= 1024*1024*1024:
			sizeStr = fmt.Sprintf("%.2f GB", dataSize/(1024*1024*1024))
		case dataSize >= 1024*1024:
			sizeStr = fmt.Sprintf("%.2f MB", dataSize/(1024*1024))
		case dataSize >= 1024:
			sizeStr = fmt.Sprintf("%.2f KB", dataSize/1024)
		default:
			sizeStr = fmt.Sprintf("%.0f 字节", dataSize)
		}
		fmt.Printf("总传输数据: %s\n", sizeStr)
		fmt.Printf("总消息数: %d\n", totalMsgs)
		fmt.Printf("总错误数: %d\n", totalErrors)
	}
}
