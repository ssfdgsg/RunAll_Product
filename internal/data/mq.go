package data

import (
	"context"
	"errors"
	"time"

	"product/api/mq"
	"product/internal/biz"
	"product/internal/conf"

	"github.com/go-kratos/kratos/v2/log"
	amqp "github.com/rabbitmq/amqp091-go"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// NewRabbitMQ dials RabbitMQ using the provided configuration.
func NewRabbitMQ(c *conf.Data, logger log.Logger) (*amqp.Connection, func(), error) {
	helper := log.NewHelper(logger)
	if c == nil || c.GetRabbitmq() == nil || c.GetRabbitmq().GetUrl() == "" {
		return nil, nil, errors.New("rabbitmq configuration is missing")
	}

	conn, err := amqp.Dial(c.GetRabbitmq().GetUrl())
	if err != nil {
		return nil, nil, err
	}

	cleanup := func() {
		if err := conn.Close(); err != nil && !errors.Is(err, amqp.ErrClosed) {
			helper.Errorf("failed to close rabbitmq connection: %v", err)
			return
		}
		helper.Info("rabbitmq connection closed")
	}

	helper.Info("rabbitmq connection established")
	return conn, cleanup, nil
}

// NewRabbitMQChannel 创建 RabbitMQ Channel 并声明 Exchange
func NewRabbitMQChannel(conn *amqp.Connection, c *conf.Data, logger log.Logger) (*amqp.Channel, error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, err
	}

	// 声明 direct exchange（与现有 Exchange 类型保持一致）
	err = ch.ExchangeDeclare(
		c.Rabbitmq.Exchange,
		"direct", // 使用 direct 类型
		true, false, false, false, nil,
	)
	if err != nil {
		return nil, err
	}

	return ch, nil
}

// mqPublisher MQ 发布器实现
type mqPublisher struct {
	conn     *amqp.Connection
	channel  *amqp.Channel
	exchange string
	log      *log.Helper
}

// NewMQPublisher 创建 MQ 发布器
func NewMQPublisher(c *conf.Data, logger log.Logger) (biz.MQPublisher, func(), error) {
	helper := log.NewHelper(logger)

	if c == nil || c.Rabbitmq == nil || c.Rabbitmq.Url == "" {
		helper.Warn("rabbitmq config not found, mq publisher disabled")
		return &noopMQPublisher{log: helper}, func() {}, nil
	}

	// 连接 RabbitMQ
	conn, err := amqp.Dial(c.Rabbitmq.Url)
	if err != nil {
		helper.Warnf("failed to connect rabbitmq: %v, using noop publisher", err)
		return &noopMQPublisher{log: helper}, func() {}, nil
	}

	// 创建 Channel
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		helper.Warnf("failed to open channel: %v, using noop publisher", err)
		return &noopMQPublisher{log: helper}, func() {}, nil
	}

	// 声明 Direct Exchange（与现有 Exchange 类型保持一致）
	err = ch.ExchangeDeclare(
		c.Rabbitmq.Exchange, // exchange name: resource.events
		"direct",            // type: direct（与现有 Exchange 一致）
		true,                // durable
		false,               // auto-deleted
		false,               // internal
		false,               // no-wait
		nil,                 // arguments
	)
	if err != nil {
		ch.Close()
		conn.Close()
		helper.Warnf("failed to declare exchange: %v, using noop publisher", err)
		return &noopMQPublisher{log: helper}, func() {}, nil
	}

	// 声明队列（确保队列存在）
	_, err = ch.QueueDeclare(
		c.Rabbitmq.Queue, // queue name: resource.instance.created
		true,             // durable
		false,            // delete when unused
		false,            // exclusive
		false,            // no-wait
		nil,              // arguments
	)
	if err != nil {
		ch.Close()
		conn.Close()
		helper.Warnf("failed to declare queue: %v, using noop publisher", err)
		return &noopMQPublisher{log: helper}, func() {}, nil
	}

	// 绑定队列到 Exchange（关键步骤！）
	err = ch.QueueBind(
		c.Rabbitmq.Queue,    // queue name
		"instance.created",  // routing key
		c.Rabbitmq.Exchange, // exchange
		false,
		nil,
	)
	if err != nil {
		ch.Close()
		conn.Close()
		helper.Warnf("failed to bind queue: %v, using noop publisher", err)
		return &noopMQPublisher{log: helper}, func() {}, nil
	}

	helper.Infof("rabbitmq connected: exchange=%s queue=%s binding=instance.created",
		c.Rabbitmq.Exchange, c.Rabbitmq.Queue)

	cleanup := func() {
		if err := ch.Close(); err != nil {
			helper.Errorf("failed to close channel: %v", err)
		}
		if err := conn.Close(); err != nil {
			helper.Errorf("failed to close connection: %v", err)
		}
		helper.Info("rabbitmq connection closed")
	}

	return &mqPublisher{
		conn:     conn,
		channel:  ch,
		exchange: c.Rabbitmq.Exchange,
		log:      helper,
	}, cleanup, nil
}

// PublishInstanceCreated 发布实例创建事件
func (p *mqPublisher) PublishInstanceCreated(ctx context.Context, spec biz.InstanceSpec) error {
	p.log.Infof("preparing to publish event: instanceID=%d userID=%s", spec.InstanceID, spec.UserID)

	event := &mq.Event{
		EventType:  mq.EventType_INSTANCE_CREATED.String(),
		InstanceId: spec.InstanceID,
		Timestamp:  timestamppb.Now(),
		UserId:     spec.UserID,
		Name:       spec.Name,
		Spec: &mq.InstanceSpec{
			Cpus:     spec.CPU,
			MemoryMb: spec.Memory,
			Gpu:      spec.GPU,
			Image:    spec.Image,
		},
	}

	// 序列化为 Protobuf
	body, err := proto.Marshal(event)
	if err != nil {
		p.log.Errorf("marshal event failed: %v", err)
		return err
	}
	p.log.Infof("event marshaled: size=%d bytes", len(body))

	// 发布消息到 Exchange，使用 routing key: instance.created
	p.log.Infof("publishing to exchange=%s routingKey=instance.created", p.exchange)
	err = p.channel.PublishWithContext(
		ctx,
		p.exchange,         // exchange: resource.events
		"instance.created", // routing key
		false,              // mandatory
		false,              // immediate
		amqp.Publishing{
			ContentType:  "application/octet-stream",
			Body:         body,
			DeliveryMode: amqp.Persistent, // 持久化
			Timestamp:    time.Now(),
		},
	)

	if err != nil {
		p.log.Errorf("publish message failed: %v", err)
		return err
	}
	return nil
}

// noopMQPublisher 空实现（当 RabbitMQ 未配置或连接失败时使用）
type noopMQPublisher struct {
	log *log.Helper
}

func (p *noopMQPublisher) PublishInstanceCreated(ctx context.Context, spec biz.InstanceSpec) error {
	if p.log != nil {
		p.log.Warnf("mq publisher not available, skipping event: instanceID=%d userID=%s", spec.InstanceID, spec.UserID)
	}
	return nil // 返回 nil 而不是错误，允许业务继续
}
