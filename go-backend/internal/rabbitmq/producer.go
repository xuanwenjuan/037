package rabbitmq

import (
	"encoding/json"
	"fmt"
	"thz-service/internal/models"

	"github.com/streadway/amqp"
)

type Producer struct {
	conn      *amqp.Connection
	channel   *amqp.Channel
	taskQueue string
	diffQueue string
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
	}, nil
}

func (p *Producer) PublishTask(task *models.TaskMessage) error {
	body, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	err = p.channel.Publish(
		"",
		p.taskQueue,
		false,
		false,
		amqp.Publishing{
			DeliveryMode: amqp.Persistent,
			ContentType:  "application/json",
			Body:         body,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to publish task: %w", err)
	}

	return nil
}

func (p *Producer) PublishDifferentialTask(task *models.DifferentialTask) error {
	body, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal differential task: %w", err)
	}

	err = p.channel.Publish(
		"",
		p.diffQueue,
		false,
		false,
		amqp.Publishing{
			DeliveryMode: amqp.Persistent,
			ContentType:  "application/json",
			Body:         body,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to publish differential task: %w", err)
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
