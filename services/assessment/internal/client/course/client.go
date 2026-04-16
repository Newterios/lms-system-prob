package course

import (
	"context"
	"fmt"
	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	coursev1 "github.com/Newterios/lms-system-prob/proto/course/v1"
	"github.com/Newterios/lms-system-prob/services/assessment/internal/model"
	"github.com/google/uuid"
)

// CourseClient calls course-svc-v2 to check enrollment.
// Implements port.EnrollmentChecker.
type CourseClient struct {
	enrollmentSvc coursev1.CourseServiceClient
	log           *slog.Logger
}

// New dials course-svc-v2 at target and returns a CourseClient.
// target is typically COURSE_GRPC_TARGET env var.
func New(target string, log *slog.Logger) (*CourseClient, error) {
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial course-svc: %w", err)
	}
	return &CourseClient{
		enrollmentSvc: coursev1.NewCourseServiceClient(conn),
		log:           log,
	}, nil
}

// IsEnrolled checks whether studentID is enrolled in courseID.
// It forwards the incoming JWT so course-svc can authenticate the call.
func (c *CourseClient) IsEnrolled(ctx context.Context, courseID, studentID uuid.UUID) (bool, error) {
	outCtx := forwardAuthMD(ctx)
	resp, err := c.enrollmentSvc.ListEnrollments(outCtx, &coursev1.ListEnrollmentsRequest{
		CourseId:  proto.String(courseID.String()),
		StudentId: proto.String(studentID.String()),
	})
	if err != nil {
		s, _ := status.FromError(err)
		if s.Code() == codes.Unavailable {
			return false, fmt.Errorf("course svc unavailable: %w", model.ErrRemoteUnavailable)
		}
		return false, fmt.Errorf("course svc: %w", err)
	}
	return len(resp.Enrollments) > 0, nil
}

// forwardAuthMD copies the incoming "authorization" metadata header into the
// outgoing gRPC context so downstream services can authenticate the call.
func forwardAuthMD(ctx context.Context) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx
	}
	vals := md.Get("authorization")
	if len(vals) == 0 {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", vals[0])
}
