package aws

import (
	"context"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
)

func ConfigWithOverride(customEndpoint string) (aws.Config, error) {
	endpointResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{
			PartitionID:   "aws",
			URL:           customEndpoint,
			SigningRegion: os.Getenv("AWS_REGION"),
		}, nil
	})
	return config.LoadDefaultConfig(context.TODO(), config.WithEndpointResolverWithOptions(endpointResolver))
}

func Config() (aws.Config, error) {
	awsEndpoint := os.Getenv("AWS_ENDPOINT")
	if len(awsEndpoint) > 0 {
		log.Printf("config: using custom global aws endpoint: %s", awsEndpoint)
		return ConfigWithOverride(awsEndpoint)
	}
	return config.LoadDefaultConfig(context.TODO(), config.WithRegion(os.Getenv("AWS_REGION")))
}
