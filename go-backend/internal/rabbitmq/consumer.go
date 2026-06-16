package rabbitmq

import (
	"encoding/json"
	"fmt"
	"log"
	"thz-service/internal/models"
	"thz-service/internal/repository"
	"thz-service/internal/websocket"

	"github.com/streadway/amqp"
)

type ResultConsumer struct {
	conn      *amqp.Connection
	channel   *amqp.Channel
	queueName string
	repo      *repository.Repository
	wsMgr     *websocket.Manager
	done      chan struct{}
}

func NewResultConsumer(url, queueName string, repo *repository.Repository, wsMgr *websocket.Manager) (*ResultConsumer, error) {
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

func (c *ResultConsumer) handleMessage(msg amqp.Delivery) {
	var result models.WorkerResultMessage
	if err := json.Unmarshal(msg.Body, &result); err != nil {
		log.Printf("Failed to unmarshal result message: %v", err)
		msg.Nack(false, false)
		return
	}

	log.Printf("Received result for analysis %s, stage: %s, status: %s",
		result.AnalysisID, result.Stage, result.Status)

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

func (c *ResultConsumer) Stop() error {
	close(c.done)
	if err := c.channel.Close(); err != nil {
		return err
	}
	return c.conn.Close()
}
