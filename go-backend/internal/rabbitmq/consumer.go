package rabbitmq

import (
	"encoding/json"
	"fmt"
	"log"
	"thz-service/internal/metrics"
	"thz-service/internal/models"
	"thz-service/internal/repository"
	"thz-service/internal/websocket"
	"time"

	"github.com/streadway/amqp"
)

type ResultConsumer struct {
	conn      *amqp.Connection
	channel   *amqp.Channel
	queueName string
	repo      *repository.Repository
	wsMgr     *websocket.Manager
	metrics   *metrics.MetricsCollector
	done      chan struct{}
}

func NewResultConsumer(url, queueName string, repo *repository.Repository, wsMgr *websocket.Manager, metrics *metrics.MetricsCollector) (*ResultConsumer, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to open channel: %w", err)
	}

	err = ch.Qos(1, 0, false)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to set QoS: %w", err)
	}

	_, err = ch.QueueDeclare(queueName, true, false, false, false, nil)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to declare queue: %w", err)
	}

	return &ResultConsumer{
		conn:      conn,
		channel:   ch,
		queueName: queueName,
		repo:      repo,
		wsMgr:     wsMgr,
		metrics:   metrics,
		done:      make(chan struct{}),
	}, nil
}

func (c *ResultConsumer) Start() error {
	msgs, err := c.channel.Consume(
		c.queueName,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to register consumer: %w", err)
	}

	go func() {
		for {
			select {
			case msg, ok := <-msgs:
				if !ok {
					return
				}
				c.handleMessage(msg)
			case <-c.done:
				return
			}
		}
	}()

	log.Printf("Result consumer started on queue: %s", c.queueName)
	return nil
}

func (c *ResultConsumer) Stop() error {
	close(c.done)
	if err := c.channel.Close(); err != nil {
		return err
	}
	return c.conn.Close()
}

func (c *ResultConsumer) StartQueueMonitor(producer *Producer, resultQueue string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			if producer != nil {
				if depth, err := producer.GetTaskQueueDepth(); err == nil {
					c.metrics.SetTaskQueueDepth(float64(depth))
				}
				if depth, err := producer.GetQueueDepth(resultQueue); err == nil {
					c.metrics.SetResultQueueDepth(float64(depth))
				}
			}
		}
	}()
}

func (c *ResultConsumer) handleMessage(msg amqp.Delivery) {
	var result models.WorkerResultMessage
	if err := json.Unmarshal(msg.Body, &result); err != nil {
		log.Printf("Failed to unmarshal result message: %v", err)
		msg.Nack(false, false)
		return
	}

	log.Printf("Received result for analysis %s, stage: %s, status: %s",
		result.AnalysisID, result.Stage, result.Status)

	if c.metrics != nil {
		if result.Performance != nil {
			if result.Performance.FFTTimeMs > 0 {
				c.metrics.RecordFFTDuration(time.Duration(result.Performance.FFTTimeMs) * time.Millisecond)
			}
			if result.Performance.ParamsTimeMs > 0 {
				c.metrics.RecordParamsDuration(time.Duration(result.Performance.ParamsTimeMs) * time.Millisecond)
			}
			if result.Performance.PredictionTimeMs > 0 {
				c.metrics.RecordPredictionDuration(time.Duration(result.Performance.PredictionTimeMs) * time.Millisecond)
			}
			if result.Performance.TotalTimeMs > 0 {
				c.metrics.RecordTotalDuration(time.Duration(result.Performance.TotalTimeMs) * time.Millisecond)
			}
		}

		if result.Status == models.StatusCompleted {
			c.metrics.TasksCompleted.Inc()
		} else if result.Status == models.StatusFailed {
			c.metrics.TasksFailed.Inc()
		} else if result.Status == models.StatusInvalid {
			c.metrics.TasksInvalid.Inc()
		}

		if result.Status == models.StatusCompleted || result.Status == models.StatusFailed || result.Status == models.StatusInvalid {
			c.metrics.TaskQueueDepth.Dec()
		}
	}

	if err := c.processResult(&result); err != nil {
		log.Printf("Failed to process result for %s: %v", result.AnalysisID, err)
		msg.Nack(false, true)
		return
	}

	msg.Ack(false)
}

func (c *ResultConsumer) processResult(result *models.WorkerResultMessage) error {
	progressMsg := &models.ProgressMessage{
		AnalysisID: result.AnalysisID,
		Status:     result.Status,
		Progress:   result.Progress,
		Message:    result.Stage,
	}

	switch result.Status {
	case models.StatusProcessing:
		if err := c.repo.UpdateAnalysisStatus(result.AnalysisID, result.Status, ""); err != nil {
			return err
		}
		progressMsg.Data = map[string]string{"stage": result.Stage}

	case models.StatusFFTDone:
		if result.FFT != nil {
			if err := c.repo.SaveFFTResult(result.AnalysisID, result.FFT); err != nil {
				return err
			}
			progressMsg.Data = result.FFT
		}

	case models.StatusParamsDone:
		if result.Params != nil {
			if err := c.repo.SaveParamsResult(result.AnalysisID, result.Params); err != nil {
				return err
			}
			progressMsg.Data = result.Params
		}

	case models.StatusCompleted:
		if err := c.repo.SaveCompleteResult(result); err != nil {
			return err
		}
		progressMsg.Data = map[string]interface{}{
			"moisture_content_percent": result.Moisture,
			"anomaly_detection":        result.AnomalyDetection,
			"performance":              result.Performance,
		}

	case models.StatusInvalid:
		if err := c.repo.SaveInvalidResult(result); err != nil {
			return err
		}
		progressMsg.Data = map[string]interface{}{
			"anomaly_detection": result.AnomalyDetection,
			"is_invalid":        true,
		}

	case models.StatusFailed:
		if err := c.repo.UpdateAnalysisStatus(result.AnalysisID, result.Status, result.Error); err != nil {
			return err
		}
		progressMsg.Message = result.Error
	}

	c.wsMgr.BroadcastProgress(result.AnalysisID, progressMsg)

	return nil
}

type DiffResultConsumer struct {
	conn      *amqp.Connection
	channel   *amqp.Channel
	queueName string
	repo      *repository.Repository
	wsMgr     *websocket.Manager
	metrics   *metrics.MetricsCollector
	done      chan struct{}
}

func NewDiffResultConsumer(url, queueName string, repo *repository.Repository, wsMgr *websocket.Manager, metrics *metrics.MetricsCollector) (*DiffResultConsumer, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to open channel: %w", err)
	}

	err = ch.Qos(1, 0, false)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to set QoS: %w", err)
	}

	_, err = ch.QueueDeclare(queueName, true, false, false, false, nil)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to declare queue: %w", err)
	}

	return &DiffResultConsumer{
		conn:      conn,
		channel:   ch,
		queueName: queueName,
		repo:      repo,
		wsMgr:     wsMgr,
		metrics:   metrics,
		done:      make(chan struct{}),
	}, nil
}

func (c *DiffResultConsumer) Start() error {
	msgs, err := c.channel.Consume(
		c.queueName,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to register consumer: %w", err)
	}

	go func() {
		for {
			select {
			case msg, ok := <-msgs:
				if !ok {
					return
				}
				c.handleDiffMessage(msg)
			case <-c.done:
				return
			}
		}
	}()

	log.Printf("Differential result consumer started on queue: %s", c.queueName)
	return nil
}

func (c *DiffResultConsumer) handleDiffMessage(msg amqp.Delivery) {
	var result struct {
		ComparisonID string                   `json:"comparison_id"`
		Status       models.AnalysisStatus    `json:"status"`
		Error        string                   `json:"error,omitempty"`
		Result       *models.DifferentialResult `json:"result,omitempty"`
	}

	if err := json.Unmarshal(msg.Body, &result); err != nil {
		log.Printf("Failed to unmarshal diff result: %v", err)
		msg.Nack(false, false)
		return
	}

	log.Printf("Received diff result for comparison %s, status: %s", result.ComparisonID, result.Status)

	switch result.Status {
	case models.StatusCompleted:
		if result.Result != nil {
			if err := c.repo.UpdateDifferentialResult(result.ComparisonID, result.Result); err != nil {
				log.Printf("Failed to update diff result: %v", err)
				msg.Nack(false, true)
				return
			}
		}
	case models.StatusFailed, models.StatusInvalid:
		if err := c.repo.UpdateDifferentialStatus(result.ComparisonID, result.Status, result.Error); err != nil {
			log.Printf("Failed to update diff status: %v", err)
			msg.Nack(false, true)
			return
		}
	}

	msg.Ack(false)

	progressMsg := &models.ProgressMessage{
		AnalysisID: result.ComparisonID,
		Status:     result.Status,
		Progress:   100,
		Message:    "differential_comparison",
	}
	if result.Result != nil {
		progressMsg.Data = result.Result
	}
	c.wsMgr.BroadcastProgress(result.ComparisonID, progressMsg)
}

func (c *DiffResultConsumer) Stop() error {
	close(c.done)
	if err := c.channel.Close(); err != nil {
		return err
	}
	return c.conn.Close()
}
