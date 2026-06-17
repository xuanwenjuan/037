package rabbitmq

import (
	"encoding/json"
	"fmt"
	"thz-service/internal/models"
	"time"

	"github.com/streadway/amqp"
)

type Producer struct {
	conn      *amqp.Connection
	channel   *amqp.Channel
	taskQueue string
	diffQueue string
	confirms  chan amqp.Confirmation
}

func NewProducer(url, taskQueue, diffQueue string) (*Producer, error) {
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

	err = ch.Confirm(false)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to enable publisher confirms: %w", err)
	}

	confirms := ch.NotifyPublish(make(chan amqp.Confirmation, 256))

	_, err = ch.QueueDeclare(taskQueue, true, false, false, false, nil)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to declare task queue: %w", err)
	}

	_, err = ch.QueueDeclare(diffQueue, true, false, false, false, nil)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to declare diff queue: %w", err)
	}

	return &Producer{
		conn:      conn,
		channel:   ch,
		taskQueue: taskQueue,
		diffQueue: diffQueue,
		confirms:  confirms,
	}, nil
}

func (p *Producer) waitForConfirm(timeout time.Duration) error {
	select {
	case confirm, ok := <-p.confirms:
		if !ok {
			return fmt.Errorf("confirm channel closed")
		}
		if !confirm.Ack {
			return fmt.Errorf("message was nacked by broker (delivery tag: %d)", confirm.DeliveryTag)
		}
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("publisher confirm timed out after %v", timeout)
	}
}

func (p *Producer) publishWithConfirm(exchange, routingKey string, body []byte, timeout time.Duration) error {
	err := p.channel.Publish(
		exchange,
		routingKey,
		false,
		false,
		amqp.Publishing{
			DeliveryMode: amqp.Persistent,
			ContentType:  "application/json",
			Body:         body,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	if err := p.waitForConfirm(timeout); err != nil {
		return fmt.Errorf("publisher confirm failed: %w", err)
	}

	return nil
}

func (p *Producer) PublishTask(task *models.TaskMessage) error {
	body, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	if err := p.publishWithConfirm("", p.taskQueue, body, 10*time.Second); err != nil {
		return fmt.Errorf("failed to publish task with confirm: %w", err)
	}

	return nil
}

func (p *Producer) PublishDifferentialTask(task *models.DifferentialTask) error {
	body, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal differential task: %w", err)
	}

	if err := p.publishWithConfirm("", p.diffQueue, body, 10*time.Second); err != nil {
		return fmt.Errorf("failed to publish differential task with confirm: %w", err)
	}

	return nil
}

func (p *Producer) GetQueueDepth(queueName string) (int, error) {
	q, err := p.channel.QueueInspect(queueName)
	if err != nil {
		return 0, fmt.Errorf("failed to inspect queue: %w", err)
	}
	return q.Messages, nil
}

func (p *Producer) GetTaskQueueDepth() (int, error) {
	return p.GetQueueDepth(p.taskQueue)
}

func (p *Producer) GetDiffQueueDepth() (int, error) {
	return p.GetQueueDepth(p.diffQueue)
}

func (p *Producer) Close() error {
	if err := p.channel.Close(); err != nil {
		return err
	}
	return p.conn.Close()
}
