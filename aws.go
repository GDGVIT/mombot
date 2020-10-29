package main

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"log"
	"os"
)

//AWS - S3

func ConnectAws() *session.Session {
	AccessKeyID := GetEnvWithKey("AWS_ACCESS_KEY_ID")
	SecretAccessKey := GetEnvWithKey("AWS_SECRET_ACCESS_KEY")
	MyRegion := GetEnvWithKey("MyRegion")
	sess, err := session.NewSession(
		&aws.Config{
			Region: aws.String(MyRegion),
			Credentials: credentials.NewStaticCredentials(
				AccessKeyID,
				SecretAccessKey,
				"",
			),
		})
	if err != nil {
		log.Println("Error :",err)
	}
	return sess
}


func AddFileToS3(s *session.Session, fileDir string) error {

	file, err := os.Open(fileDir)
	if err != nil {
		return err
	}
	defer file.Close()
	log.Println(GetEnvWithKey("S3Bucket"))
	uploader := s3manager.NewUploader(s)
	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket:               aws.String(GetEnvWithKey("S3Bucket")),
		Key:                  aws.String(fileDir),
		Body:                 file,
		ServerSideEncryption: aws.String("AES256"),
	})
	return err
}

func AddtoS3(fileDir string) error{
	log.Println("FileDIR: ",fileDir)
	s := ConnectAws()
	// Upload
	err := AddFileToS3(s, fileDir)
	if err != nil {
		log.Println(err)
		return err
	}
	return nil
}
