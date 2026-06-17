package cache

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type CacheService struct {
	client *redis.Client
	ttl    time.Duration
}

type WaveformSignature struct {
	MD5             string    `json:"md5"`
	TimePointsCount  []float64 `json:"time_points"`
	SampleFieldCount  []float64 `json:"sample_field"`
}

type CacheResult struct {
	Exists      bool        `json:"exists"`
	MD5         string      `json:"md5"`
	AnalysisID  string      `json:"analysis_id"`
	Data        interface{} `json:"data,omitempty"`
	CachedAt    time.Time   `json:"cached_at,omitempty"`
	HitCount    int64       `json:"hit_count,omitempty"`
}

func NewCacheService(redisURL string, ttl time.Duration) (*CacheService, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("invalid Redis URL: %w", err)
	}

	client := redis.NewClient(opt)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &CacheService{
		client: client,
		ttl:    ttl,
	}, nil
}

func ComputeWaveformMD5(timePoints, sampleField []float64) string {
	h := md5.New()

	timeBytes, _ := json.Marshal(timePoints)
	sampleBytes, _ := json.Marshal(sampleField)

	h.Write(timeBytes)
	h.Write(sampleBytes)

	return hex.EncodeToString(h.Sum(nil))
}

func (c *CacheService) ComputeWaveformMD5(timePoints, sampleField []float64) string {
	return ComputeWaveformMD5(timePoints, sampleField)
}

func (c *CacheService) Get(ctx context.Context, key string) (*CacheResult, error) {
	result := &CacheResult{MD5: key}

	data, err := c.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return result, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cache get failed: %w", err)
	}

	var cachedData struct {
		AnalysisID string      `json:"analysis_id"`
		Data       interface{} `json:"data"`
		CachedAt   time.Time   `json:"cached_at"`
		HitCount   int64       `json:"hit_count"`
	}

	if err := json.Unmarshal([]byte(data), &cachedData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cached data: %w", err)
	}

	result.Exists = true
	result.AnalysisID = cachedData.AnalysisID
	result.Data = cachedData.Data
	result.CachedAt = cachedData.CachedAt
	result.HitCount = cachedData.HitCount

	c.client.Incr(ctx, fmt.Sprintf("%s:hits", key))

	return result, nil
}

func (c *CacheService) Set(ctx context.Context, key string, data interface{}) error {
	cachedData := struct {
		AnalysisID string      `json:"analysis_id"`
		Data       interface{} `json:"data"`
		CachedAt   time.Time   `json:"cached_at"`
		HitCount   int64       `json:"hit_count"`
	}{
		Data:      data,
		CachedAt:  time.Now(),
		HitCount:  0,
	}

	body, err := json.Marshal(cachedData)
	if err != nil {
		return fmt.Errorf("failed to marshal cache data: %w", err)
	}

	return c.client.Set(ctx, key, body, c.ttl).Err()
}

func (c *CacheService) SetWithAnalysis(ctx context.Context, key string, analysisID string, data interface{}) error {
	cachedData := struct {
		AnalysisID string      `json:"analysis_id"`
		Data       interface{} `json:"data"`
		CachedAt   time.Time   `json:"cached_at"`
		HitCount   int64       `json:"hit_count"`
	}{
		AnalysisID: analysisID,
		Data:       data,
		CachedAt:   time.Now(),
		HitCount:   0,
	}

	body, err := json.Marshal(cachedData)
	if err != nil {
		return fmt.Errorf("failed to marshal cache data: %w", err)
	}

	return c.client.Set(ctx, key, body, c.ttl).Err()
}

func (c *CacheService) Delete(ctx context.Context, key string) error {
	return c.client.Del(ctx, key).Err()
}

func (c *CacheService) GetHitCount(ctx context.Context, key string) (int64, error) {
	return c.client.Get(ctx, fmt.Sprintf("%s:hits", key)).Int64()
}

func (c *CacheService) GetStats(ctx context.Context) (map[string]interface{}, error) {
	keys, err := c.client.Keys(ctx, "thz:cache:*").Result()
	if err != nil {
		return nil, err
	}

	totalHits := int64(0)
	for _, key := range keys {
		hitCount, _ := c.GetHitCount(ctx, key)
		totalHits += hitCount
	}

	return map[string]interface{}{
		"total_entries": len(keys),
		"total_hits":    totalHits,
		"hit_rate":     float64(totalHits) / float64(len(keys)+1) * 100,
	}, nil
}

func (c *CacheService) Close() error {
	return c.client.Close()
}

func (c *CacheService) ClearAll(ctx context.Context) error {
	keys, err := c.client.Keys(ctx, "thz:cache:*").Result()
	if err != nil {
		return err
	}
	if len(keys) > 0 {
		return c.client.Del(ctx, keys...).Err()
	}
	return nil
}
