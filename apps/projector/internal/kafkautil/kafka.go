package kafkautil

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

func NewProducer(brokers []string) *kafka.Producer {
	p, err := kafka.NewProducer(&kafka.ConfigMap{
		"bootstrap.servers":            strings.Join(brokers, ","),
		"acks":                         0,
		"go.delivery.reports":          false,
		"queue.buffering.max.messages": 200000,
		"queue.buffering.max.kbytes":   262144,
		"linger.ms":                    5,
		"batch.num.messages":           10000,
	})
	if err != nil {
		slog.Default().Error("failed to create kafka producer", "error", err)
		panic(err)
	}
	return p
}

func UniqueGroupID(prefix string) string {
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), rand.Int63())
}

func PublishJSON(producer *kafka.Producer, topic string, key []byte, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	msg := &kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
		Key:            key,
		Value:          b,
		Timestamp:      time.Now().UTC(),
	}
	return producer.Produce(msg, nil)
}
