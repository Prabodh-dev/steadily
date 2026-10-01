package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"
)

type EchoResponse struct {
	Address      string `json:"address"`
	RequestCount uint64 `json:"request_count"`
}

func main() {
	log.Println("Starting demo-kill test...")

	log.Println("Bringing up Docker Compose stack...")
	cmd := exec.Command("docker", "compose", "up", "-d", "--build")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("failed to bring up docker compose: %v", err)
	}

	defer func() {
		log.Println("Tearing down Docker Compose stack...")
		downCmd := exec.Command("docker", "compose", "down", "-v")
		_ = downCmd.Run()
	}()

	log.Println("Waiting for steadily load balancer to be healthy...")
	client := &http.Client{Timeout: 3 * time.Second}
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

	log.Println("Steadily is ready. Firing 250 requests while killing echo1 mid-traffic...")

	totalRequests := 250
	killAt := 75

	var mu sync.Mutex
	backendCounts := make(map[string]int)
	phaseBeforeKill := make(map[string]int)
	phaseAfterKill := make(map[string]int)

	var successCount int
	var failCount int

	var wg sync.WaitGroup

	for i := 1; i <= totalRequests; i++ {
		wg.Add(1)
		reqNum := i

		go func(id int) {
			defer wg.Done()

			if id == killAt {
				log.Println("MID-TRAFFIC EVENT: Killing echo1 backend container with docker kill...")
				killCmd := exec.Command("docker", "kill", "steadily-echo1-1")
				if err := killCmd.Run(); err != nil {
					altKillCmd := exec.Command("docker", "compose", "kill", "echo1")
					_ = altKillCmd.Run()
				}
				log.Println("echo1 container killed.")
			}

			resp, err := client.Get("http://localhost:8080/")
			if err != nil {
				mu.Lock()
				failCount++
				mu.Unlock()
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				mu.Lock()
				failCount++
				mu.Unlock()
				return
			}

			body, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				mu.Lock()
				failCount++
				mu.Unlock()
				return
			}

			var echoResp EchoResponse
			if err := json.Unmarshal(body, &echoResp); err != nil {
				mu.Lock()
				failCount++
				mu.Unlock()
				return
			}

			mu.Lock()
			successCount++
			backendCounts[echoResp.Address]++
			if id < killAt {
				phaseBeforeKill[echoResp.Address]++
			} else {
				phaseAfterKill[echoResp.Address]++
			}
			mu.Unlock()
		}(reqNum)

		time.Sleep(20 * time.Millisecond)
	}

	wg.Wait()

	fmt.Println("\n================ DEMO RESULT ================")
	fmt.Printf("Total Requests Sent : %d\n", totalRequests)
	fmt.Printf("Total Succeeded     : %d\n", successCount)
	fmt.Printf("Total Failed        : %d\n", failCount)
	fmt.Println("--------------------------------------------")
	fmt.Println("Overall Request Distribution:")
	for b, count := range backendCounts {
		fmt.Printf("  Backend %-15s : %d requests\n", b, count)
	}
	fmt.Println("--------------------------------------------")
	fmt.Println("Before Backend Kill:")
	for b, count := range phaseBeforeKill {
		fmt.Printf("  Backend %-15s : %d requests\n", b, count)
	}
	fmt.Println("After Backend Kill:")
	for b, count := range phaseAfterKill {
		fmt.Printf("  Backend %-15s : %d requests\n", b, count)
	}
	fmt.Println("============================================")

	if failCount == 0 {
		log.Println("DEMO SUCCESS: Zero dropped requests during mid-traffic backend kill!")
	} else {
		log.Fatalf("DEMO FAILED: %d requests failed during backend kill!", failCount)
	}
}
