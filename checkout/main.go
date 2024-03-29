package main

import (
	"context"
	"fmt"
	"log"
	"net"

	pb "github.com/kaa-it/golang-trace-2/proto"
	"github.com/kaa-it/golang-trace-2/utils"
	"github.com/streadway/amqp"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
)

func main() {
	jaegerAddress := utils.EnvString("JAEGER_ADDRESS", "localhost")
	jaegerPort := utils.EnvString("JAEGER_PORT", "6831")
	grpcAddress := utils.EnvString("GRPC_ADDRESS", "localhost:8080")
	amqpUser := utils.EnvString("RABBITMQ_USER", "guest")
	amqpPass := utils.EnvString("RABBITMQ_PASS", "guest")
	amqpHost := utils.EnvString("RABBITMQ_HOST", "localhost")
	amqpPort := utils.EnvString("RABBITMQ_PORT", "5672")

	err := utils.SetGlobalTracer("checkout", jaegerAddress, jaegerPort)
	if err != nil {
		log.Fatalf("failed to create tracer: %v", err)
	}

	channel, closeConn := utils.ConnectAmqp(amqpUser, amqpPass, amqpHost, amqpPort)
	defer closeConn()

	lis, err := net.Listen("tcp", grpcAddress)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer(
		grpc.UnaryInterceptor(otelgrpc.UnaryServerInterceptor()),
		grpc.StreamInterceptor(otelgrpc.StreamServerInterceptor()))

	pb.RegisterCheckoutServer(s, &server{channel: channel})

	log.Printf("GRPC server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

type server struct {
	pb.UnimplementedCheckoutServer
	channel *amqp.Channel
}

func (s *server) DoCheckout(ctx context.Context, rq *pb.CheckoutRequest) (*pb.CheckoutResponse, error) {
	messageName := "checkout.processed"

	// Create a new span (child of the trace id) to inform the publishing of the message
	tr := otel.Tracer("amqp")
	amqpContext, messageSpan := tr.Start(ctx, fmt.Sprintf("AMQP - publish - %s", messageName))
	defer messageSpan.End()

	// Inject the context in the headers
	headers := utils.InjectAMQPHeaders(amqpContext)
	msg := amqp.Publishing{Headers: headers}
	err := s.channel.Publish("exchange", messageName, false, false, msg)
	if err != nil {
		log.Fatal(err)
	}

	response := &pb.CheckoutResponse{TotalAmount: 1234}

	// Example on how to log specific events for a span
	span := trace.SpanFromContext(ctx)
	span.AddEvent(fmt.Sprintf("response: %v", response))

	return response, nil
}
