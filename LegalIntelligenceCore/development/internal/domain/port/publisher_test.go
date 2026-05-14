package port

import "context"

type fakeStatusPublisher struct{}

func (fakeStatusPublisher) PublishStatus(context.Context, LICStatusChangedEvent) error { return nil }

type fakeUncertaintyPublisher struct{}

func (fakeUncertaintyPublisher) PublishClassificationUncertain(
	context.Context, ClassificationUncertain,
) error {
	return nil
}

type fakeDLQPublisher struct{}

func (fakeDLQPublisher) PublishDLQ(context.Context, DLQTopic, LICDLQEnvelope) error { return nil }

var (
	_ StatusPublisherPort      = (*fakeStatusPublisher)(nil)
	_ UncertaintyPublisherPort = (*fakeUncertaintyPublisher)(nil)
	_ DLQPublisherPort         = (*fakeDLQPublisher)(nil)
)
