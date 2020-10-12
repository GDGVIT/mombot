package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"math/rand"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/pion/webrtc/v3/pkg/media/oggwriter"
)

// Variables used for command line parameters

const charset = "abcdefghijklmnopqrstuvwxyz" +
	"ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

var seededRand *rand.Rand = rand.New(
	rand.NewSource(time.Now().UnixNano()))

func stringWithCharset(length int, charset string) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}

func randomString(length int) string {
	return stringWithCharset(length, charset)
}

type item struct {
	id          string
	channelName string
	channelID   string
	status      bool
}

type config struct {
	Token     string `env:"Token"`
	ChannelID string `env:"ChannelID"`
	GuildID   string `env:"GuildID"`
}

func createPionRTPPacket(p *discordgo.Packet) *rtp.Packet {
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

func goDotEnvVariable(key string) string {
	err := godotenv.Load(".env")
	if err != nil {
		log.Fatalf("Error loading .env file")
	}
	return os.Getenv(key)
}

func handleVoice(c chan *discordgo.Packet, id string) {
	files := make(map[string]media.Writer)
	for p := range c {
		file, ok := files[id]
		if !ok {
			var err error
			file, err = oggwriter.New(fmt.Sprintf("%s.ogg", id), 48000, 2)
			if err != nil {
				fmt.Printf("failed to create file %s.ogg, giving up on recording: %v\n", id, err)
				return
			}
			files[id] = file
		}
		// Construct pion RTP packet from DiscordGo's type.
		rtp := createPionRTPPacket(p)
		err := file.WriteRTP(rtp)
		if err != nil {
			fmt.Printf("failed to write to file %s.ogg, giving up on recording: %v\n", id, err)
		}
	}

	// Once we made it here, we're done listening for packets. Close all files
	for _, f := range files {

		f.Close()
	}
}

func handleConfig(status bool, channelName string) {
	Token := goDotEnvVariable("Token")
	GuildID := goDotEnvVariable("GuildID")
	ChannelID := goDotEnvVariable("ChannelID")
	s, err := discordgo.New("Bot " + Token)
	channels, _ := s.GuildChannels(GuildID)
	for _, c := range channels {
		fmt.Println(c)
		if c.Name == channelName {
			ChannelID = c.ID

		}
	}
	if err != nil {
		fmt.Println("error creating Discord session:", err)
		return
	}
	defer s.Close()

	// We only really care about receiving voice state updates.
	s.Identify.Intents = discordgo.MakeIntent(discordgo.IntentsGuildVoiceStates)

	err = s.Open()
	if err != nil {
		fmt.Println("error opening connection:", err)
		return
	}
	v, err := s.ChannelVoiceJoin(GuildID, ChannelID, true, false)
	if err != nil {
		fmt.Println("failed to join voice channel:", err)
		return
	}

	if status {
		id := randomString(6)
		sess := session.Must(session.NewSessionWithOptions(session.Options{
			SharedConfigState: session.SharedConfigEnable,
			Config: aws.Config{
				Region: aws.String("ap-south-1"),
			},
		}))
		svc := dynamodb.New(sess)
		item := item{
			id:          id,
			channelName: channelName,
			channelID:   ChannelID,
			status:      false,
		}

		av, err := dynamodbattribute.MarshalMap(item)
		if err != nil {
			fmt.Println("Got error marshalling new item:")
			fmt.Println(err.Error())
			os.Exit(1)
		}

		tableName := "MoMBOT"

		input := &dynamodb.PutItemInput{
			Item:      av,
			TableName: aws.String(tableName),
		}

		_, err = svc.PutItem(input)
		if err != nil {
			fmt.Println("Got error calling PutItem:")
			fmt.Println(err.Error())
			os.Exit(1)
		}
		handleVoice(v.OpusRecv, id)
	} else {
		close(v.OpusRecv)
		v.Close()
	}
}

func main() {
	handleMessages()
}

func handleMessages() {
	Token := goDotEnvVariable("Token")
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
