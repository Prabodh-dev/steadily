package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

type EchoResponse struct {
	Address      string `json:"address"`
	RequestCount uint64 `json:"request_count"`
}

func main() {
	log.Println("Starting Steadily chaos test script...")

	log.Println("Ensuring Docker Compose stack is running...")
	cmd := exec.Command("docker", "compose", "up", "-d", "--build")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("failed to bring up docker compose: %v", err)
	}

	defer func() {
		log.Println("Restoring all containers in Docker Compose stack...")
		upCmd := exec.Command("docker", "compose", "up", "-d")
		_ = upCmd.Run()
	}()

	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 50,
			DisableKeepAlives:   false,
		},
	}

	ready := false
	for i := 0; i < 30; i++ {
		resp, err := client.Get("http://localhost:8080/")
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			ready = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !ready {
		log.Fatalf("steadily load balancer failed to become ready on http://localhost:8080/")
	}

	log.Println("Steadily is healthy. Launching continuous traffic while triggering random container kills...")

	duration := 15 * time.Second
	stopChan := make(chan struct{})
	var successCount int64
	var failCount int64
	var totalRequests int64

	var mu sync.Mutex
	failDetails := make([]string, 0)
	backendDistribution := make(map[string]int64)

	concurrency := 15
	var wg sync.WaitGroup

	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for {
				select {
				case <-stopChan:
					return
				default:
					atomic.AddInt64(&totalRequests, 1)
					resp, err := client.Get("http://localhost:8080/")
					if err != nil {
						atomic.AddInt64(&failCount, 1)
						mu.Lock()
						failDetails = append(failDetails, fmt.Sprintf("Worker %d HTTP error: %v", workerID, err))
						mu.Unlock()
						time.Sleep(20 * time.Millisecond)
						continue
					}

					body, readErr := io.ReadAll(resp.Body)
					_ = resp.Body.Close()

					if resp.StatusCode == http.StatusOK && readErr == nil {
						var echoResp EchoResponse
						if jsonErr := json.Unmarshal(body, &echoResp); jsonErr == nil {
							atomic.AddInt64(&successCount, 1)
							mu.Lock()
							backendDistribution[echoResp.Address]++
							mu.Unlock()
						} else {
							atomic.AddInt64(&failCount, 1)
						}
					} else {
						atomic.AddInt64(&failCount, 1)
						mu.Lock()
						failDetails = append(failDetails, fmt.Sprintf("Worker %d status %d", workerID, resp.StatusCode))
						mu.Unlock()
					}

					time.Sleep(15 * time.Millisecond)
				}
			}
		}(w)
	}

	chaosTargets := []string{"echo1", "echo2", "echo3"}
	chaosTimer := time.NewTimer(2 * time.Second)
	testEndTime := time.Now().Add(duration)

	var chaosEventsCount int

loop:
	for {
		select {
		case <-chaosTimer.C:
			if time.Now().After(testEndTime) {
				break loop
			}
			target := chaosTargets[rand.Intn(len(chaosTargets))]
			chaosEventsCount++
			log.Printf("CHAOS EVENT %d: Killing container %s...", chaosEventsCount, target)

			killCmd := exec.Command("docker", "compose", "kill", target)
			_ = killCmd.Run()

			time.Sleep(2 * time.Second)

			log.Printf("CHAOS EVENT %d: Restarting container %s...", chaosEventsCount, target)
			startCmd := exec.Command("docker", "compose", "start", target)
			_ = startCmd.Run()

			chaosTimer.Reset(3 * time.Second)
		default:
			if time.Now().After(testEndTime) {
				break loop
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	close(stopChan)
	wg.Wait()

	total := atomic.LoadInt64(&totalRequests)
	succ := atomic.LoadInt64(&successCount)
	fail := atomic.LoadInt64(&failCount)

	errorRate := 0.0
	if total > 0 {
		errorRate = float64(fail) / float64(total) * 100.0
	}

	fmt.Println("\n================ CHAOS TEST RESULTS ================")
	fmt.Printf("Duration               : %s\n", duration.String())
	fmt.Printf("Chaos Kill Events      : %d\n", chaosEventsCount)
	fmt.Printf("Total Requests Sent    : %d\n", total)
	fmt.Printf("Successful Requests    : %d\n", succ)
	fmt.Printf("Failed Requests        : %d\n", fail)
	fmt.Printf("Overall Error Rate     : %.2f%%\n", errorRate)
	fmt.Println("--------------------------------------------------")
	fmt.Println("Backend Traffic Distribution:")
	for b, cnt := range backendDistribution {
		fmt.Printf("  Backend %-15s : %d requests\n", b, cnt)
	}
	fmt.Println("--------------------------------------------------")

	if len(failDetails) > 0 {
		fmt.Println("Sample Failure Details (in-flight during kill milliseconds):")
		maxShow := 5
		if len(failDetails) < maxShow {
			maxShow = len(failDetails)
		}
		for i := 0; i < maxShow; i++ {
			fmt.Printf("  - %s\n", failDetails[i])
		}
	}
	fmt.Println("==================================================")

	if errorRate > 5.0 {
		log.Fatalf("CHAOS TEST FAILED: Error rate %.2f%% exceeded acceptable threshold", errorRate)
	} else {
		log.Println("CHAOS TEST SUCCESSFUL: Mid-traffic resilience verified!")
	}
}
