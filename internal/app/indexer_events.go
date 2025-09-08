package app

import (
	"context"

	"github.com/charmbracelet/crush/internal/pubsub"
)

type IndexerEventType string

const (
	IndexerEventInfo  IndexerEventType = "info"
	IndexerEventWarn  IndexerEventType = "warn"
	IndexerEventError IndexerEventType = "error"
)

type IndexerEvent struct {
	Type    IndexerEventType
	Message string
	Path    string
}

var indexerBroker = pubsub.NewBroker[IndexEREventWrapper]()

// wrapper to avoid generics import issues across packages in older Go versions
type IndexEREventWrapper = IndexerEvent

func SubscribeIndexerEvents(ctx context.Context) <-chan pubsub.Event[IndexEREventWrapper] {
	return indexerBroker.Subscribe(ctx)
}

func indexerPublish(ev IndexerEvent) {
	indexerBroker.Publish(pubsub.UpdatedEvent, ev)
}

func IndexerInfo(msg, path string) {
	indexerPublish(IndexerEvent{Type: IndexerEventInfo, Message: msg, Path: path})
}
func IndexerWarn(msg, path string) {
	indexerPublish(IndexerEvent{Type: IndexerEventWarn, Message: msg, Path: path})
}
func IndexerError(msg, path string) {
	indexerPublish(IndexerEvent{Type: IndexerEventError, Message: msg, Path: path})
}
