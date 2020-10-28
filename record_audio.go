package main

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/pion/webrtc/v3/pkg/media/oggwriter"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

var ctx = context.Background()
var dgVoice *discordgo.Session
var connection *discordgo.VoiceConnection
var fileLocation uint32



func main() {
	LoadEnv()
	Token := GetEnvWithKey("Token")
	var err error
	dgVoice, err = discordgo.New("Bot " + Token)
	if err != nil {
		fmt.Println("error creating Discord session,", err)
		return
	}
	defer dgVoice.Close()
	dgVoice.Identify.Intents = discordgo.MakeIntent(discordgo.IntentsGuildVoiceStates)
	err = dgVoice.Open()
	if err != nil {
		fmt.Println("error opening connection:", err)
		return
	}
	handleMessages()
}

func GetEnvWithKey(key string) string {
	return os.Getenv(key)
}

func LoadEnv() {
	err := godotenv.Load(".env")
	if err != nil {
		log.Fatalf("Error loading .env file")
		os.Exit(1)
	}
}



func createPitonRTPPacket(p *discordgo.Packet) *rtp.Packet {
	return &rtp.Packet{
		Header: rtp.Header{
			Version: 2,
			// Taken from Discord voice docs
			PayloadType:    0x78,
			SequenceNumber: p.Sequence,
			Timestamp:      p.Timestamp,
			SSRC:           p.SSRC,
		},
		Payload: p.Opus,
	}
}


func handleVoice(c chan *discordgo.Packet, channel string) {
	files := make(map[uint32]media.Writer)
	for p := range c {
		file, ok := files[p.SSRC]
		if !ok {
			fileLocation= p.SSRC
			var err error
			file, err = oggwriter.New(fmt.Sprintf("%d.ogg", p.SSRC), 48000, 2)
			if err != nil {
				fmt.Printf("failed to create file %d.ogg, giving up on recording: %v\n", p.SSRC, err)
				return
			}
			files[p.SSRC] = file
		}
		rtp := createPitonRTPPacket(p)
		err := file.WriteRTP(rtp)
		if err != nil {
			fmt.Printf("failed to write to file %d.ogg, giving up on recording: %v\n", p.SSRC, err)
		}
	}

	for _, f := range files {
		f.Close()
		fmt.Println(fileLocation)
		fmt.Println("Closed file")
	}
}

func handleConfig(status bool, channelName string) {


	if status {
		GuildID := GetEnvWithKey("GuildID")
		ChannelID := GetEnvWithKey("ChannelID")
		channels, _ := dgVoice.GuildChannels(GuildID)
		for _, c := range channels {
			if c.Name == channelName {
				ChannelID = c.ID
			}
		}

		v, err := dgVoice.ChannelVoiceJoin(GuildID, ChannelID, true, false)
		if err != nil {
			fmt.Println("failed to join voice channel:", err)
			return
		}
		connection=v
		fmt.Println("JOINING CHANNEL")
		handleVoice(v.OpusRecv,channelName)
	} else {
		fmt.Println("LEAVING CHANNEL")
		close(connection.OpusRecv)
		connection.Close()
		connection.Disconnect()
		AddtoS3(fmt.Sprint(fileLocation)+".ogg")
	}
}

func handleMessages() {
	Token := GetEnvWithKey("Token")
	dg, err := discordgo.New("Bot " + Token)
	if err != nil {
		fmt.Println("error creating Discord session,", err)
		return
	}
	dg.AddHandler(messageCreate)
	dg.Identify.Intents = discordgo.MakeIntent(discordgo.IntentsGuildMessages)
	err = dg.Open()
	if err != nil {
		fmt.Println("error opening connection,", err)
		return
	}
	fmt.Println("Bot is now running.  Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-sc
	dg.Close()
}

func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}
	stri := strings.Split(m.Content, " ")
	if len(stri) >= 1 {
		if stri[1] == "join" {
			stri = strings.Split(m.Content, "join ")
			s.ChannelMessageSend(m.ChannelID, "Got it! Joining Channel: "+stri[1])
			handleConfig(true, stri[1])
		} else if stri[1] == "leave" {
			stri = strings.Split(m.Content, "leave ")
			s.ChannelMessageSend(m.ChannelID, "Got it! Leaving Channel: "+stri[1])
			handleConfig(false, stri[1])
		}
	}

}




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
		fmt.Println("Error :",err)
	}
	return sess
}


func AddFileToS3(s *session.Session, fileDir string) error {

	file, err := os.Open(fileDir)
	if err != nil {
		return err
	}
	defer file.Close()
	fmt.Println(GetEnvWithKey("S3Bucket"))
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
	fmt.Println("FileDIR: ",fileDir)
	s := ConnectAws()
	// Upload
	err := AddFileToS3(s, fileDir)
	if err != nil {
		fmt.Println(err)
		return err
	}
	return nil
}
