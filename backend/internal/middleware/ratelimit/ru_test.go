package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRUCostAndReject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	limiter := NewRULimiter(0, 6)
	router := gin.New()
	router.Use(limiter.Middleware())
	router.GET("/feed/listLatest", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.POST("/video/publish", func(c *gin.Context) { c.Status(http.StatusOK) })

	read := httptest.NewRequest(http.MethodGet, "/feed/listLatest", nil)
	readRec := httptest.NewRecorder()
	router.ServeHTTP(readRec, read)
	if readRec.Code != http.StatusOK {
		t.Fatalf("read status=%d", readRec.Code)
	}
	if len(limiter.samples) != 1 || limiter.samples[0].cost < 0 || limiter.samples[0].finished.IsZero() {
		t.Fatalf("sample=%+v", limiter.samples)
	}
	if limiter.tokens != 5 {
		t.Fatalf("tokens after read=%v", limiter.tokens)
	}

	write := httptest.NewRequest(http.MethodPost, "/video/publish", nil)
	writeRec := httptest.NewRecorder()
	router.ServeHTTP(writeRec, write)
	if writeRec.Code != http.StatusTooManyRequests {
		t.Fatalf("write status=%d", writeRec.Code)
	}
	if len(limiter.samples) != 1 {
		t.Fatalf("rejected request was recorded, samples=%d", len(limiter.samples))
	}
}

func TestDelayRatioDoublesCost(t *testing.T) {
	limiter := NewRULimiter(0, 12)
	now := time.Now()
	old := now.Add(-time.Minute)
	for i := 0; i < 400; i++ {
		limiter.samples = append(limiter.samples, requestSample{cost: 10 * time.Millisecond, finished: old})
	}
	for i := 0; i < 20; i++ {
		limiter.samples = append(limiter.samples, requestSample{cost: 100 * time.Millisecond, finished: now})
	}
	if ratio := limiter.delayRatio(now); ratio <= 1 {
		t.Fatalf("ratio=%v", ratio)
	}
	if !limiter.allow(writeRU) {
		t.Fatal("doubled write should fit in 12 tokens")
	}
	if limiter.tokens != 0 {
		t.Fatalf("tokens=%v", limiter.tokens)
	}
	if limiter.allow(readRU) {
		t.Fatal("bucket should be empty")
	}
}
