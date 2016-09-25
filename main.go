package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"

	"golang.org/x/crypto/ssh"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

func main() {
	svc := ec2.New(session.New(), &aws.Config{Region: aws.String("us-west-2")})

	// Find a running instance to use
	inst, err := findRunningInstance(svc)
	if err != nil {
		// No running instance exists, create one
		rsv, err := createInstance(svc)
		check(err)

		inst = rsv.Instances[0]
		fmt.Printf("Created new instance\n")

		// Wait until the instance is up and running
		waitForInstanceToRun(svc, inst.InstanceId)

		fmt.Printf("Instance is up and running\n")
	} else {
		fmt.Printf("Running instance found\n")
	}

	fmt.Printf("Using instance '%s'\n", *inst.InstanceId)

	// Execute command
	res, err := execCmd(inst, "whoami")
	check(err)
	fmt.Printf("Whoami: %s", *res)
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}

func createInstance(svc *ec2.EC2) (*ec2.Reservation, error) {
	params := &ec2.RunInstancesInput{
		ImageId:      aws.String("ami-d732f0b7"), // Ubuntu
		InstanceType: aws.String("t2.micro"),
		MaxCount:     aws.Int64(1),
		MinCount:     aws.Int64(1),
		KeyName:      aws.String("cc_assignment0"),
	}
	return svc.RunInstances(params)
}

func findRunningInstance(svc *ec2.EC2) (*ec2.Instance, error) {
	resp, err := svc.DescribeInstances(nil)
	if err != nil {
		return nil, err
	}

	for _, res := range resp.Reservations {
		for _, inst := range res.Instances {
			if *inst.State.Name == "running" {
				return inst, err
			}
		}
	}

	return nil, fmt.Errorf("Could not find running instance.")
}

func waitForInstanceToRun(svc *ec2.EC2, id *string) error {
	params := &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			&ec2.Filter{
				Name:   aws.String("instance-id"),
				Values: []*string{id},
			},
		},
	}

	return svc.WaitUntilInstanceRunning(params)
}

func execCmd(inst *ec2.Instance, cmd string) (*string, error) {
	// Open PEM file
	pemPath := os.Getenv("PEM_PATH")
	pemBytes, err := ioutil.ReadFile(pemPath)
	if err != nil {
		return nil, err
	}

	// Obtain private key
	signer, err := ssh.ParsePrivateKey(pemBytes)
	if err != nil {
		return nil, err
	}

	// Connect to the remote server and perform the SSH handshake
	config := &ssh.ClientConfig{
		User: "ubuntu",
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
	}
	addr := fmt.Sprintf("%s:%d", *inst.PublicIpAddress, 22)
	conn, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	session, err := conn.NewSession()
	if err != nil {
		return nil, err
	}

	defer session.Close()
	var stdoutBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	err = session.Run(cmd)
	check(err)

	return aws.String(stdoutBuf.String()), nil
}
