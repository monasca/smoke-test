package main

import (
	"io"
	"net/http"
	"time"
	"github.com/gophercloud/gophercloud/openstack"
	"os"
	"fmt"
	"github.com/gophercloud/gophercloud"
	"github.com/monasca/golang-monascaclient/monascaclient"
	"strconv"
	"github.com/monasca/golang-monascaclient/monascaclient/models"
)

var webhookTriggered bool = false
var testsSucceeded int = 0
var totalTests int = 6

func handleWebhook(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, "Received WEBHOOK")
	webhookTriggered = true
}

func getToken(authOptions *gophercloud.AuthOptions) (string, error) {
	openstackProvider, err := openstack.AuthenticatedClient(*authOptions)
	if err != nil {
		return "", err
	}
	return openstackProvider.TokenID, nil
}

func initializeMonascaClient(token, monascaURL string)(){
	timeoutSetting := os.Getenv("TIMEOUT")
	var timeout int
	if timeoutSetting == "" {
		timeout = 5
	} else {
		var err error
		timeout, err = strconv.Atoi(timeoutSetting)
		if err != nil {
			timeout = 5
		}
	}
	monascaclient.SetBaseURL(monascaURL)
	monascaclient.SetTimeout(timeout)
	headers := http.Header{}
	headers.Add("X-Auth-Token", token)
	monascaclient.SetHeaders(headers)
}

func testMeasurementsFlowing(){
	//mergeMetrics := false
	metricName := "pod.cpu.total_time_sec"
	startTime := time.Now()
	startTime = startTime.Add(-3 * time.Minute)
	groupBy := "*"
	measurementQuery := models.MeasurementQuery{Name:&metricName, StartTime:&startTime, GroupBy:&groupBy}
	measurements, err := monascaclient.GetMeasurements(&measurementQuery)
	if err != nil {
		fmt.Printf("FAILED - Error getting measurements from API test failed %s\n", err.Error())
		return
	}
	if len(measurements.Elements) == 0 {
		fmt.Println("FAILED - No current measurements found for pod.cpu.total_time_sec")
		return
	}
	fmt.Println("SUCCESS")
	testsSucceeded++
}

func testCreateNotification(webhookAddress string) string {
	name := "smoke_test_notification"
	notificationType := "webhook"
	requestBody := models.NotificationRequestBody{Name:&name, Type:&notificationType, Address:&webhookAddress}
	notificationResponse, err := monascaclient.CreateNotificationMethod(&requestBody)
	if err != nil {
		fmt.Printf("FAILED - Error creating notification method %s\n", err.Error())
		return ""
	}
	fmt.Println("SUCCESS")
	testsSucceeded++
	return notificationResponse.ID
}

func testCreateAlarmDefinition(notificationID string) string {
	name := "smoke_test_alarm"
	expression := "smoke_test_metric>0"
	alarmDefActions := []string{notificationID}
	requestBody := models.AlarmDefinitionRequestBody{Name:&name, Expression:&expression, AlarmActions:&alarmDefActions, UndeterminedActions:&alarmDefActions, OkActions:&alarmDefActions}
	alarmDefinitionResponse, err := monascaclient.CreateAlarmDefinition(&requestBody)
	if err != nil {
		fmt.Printf("FAILED - Error creating alarm definition %s\n", err.Error())
		return ""
	}
	fmt.Println("SUCCESS")
	testsSucceeded++
	return alarmDefinitionResponse.ID
}

func testCreateMetric(value float64) {
	now := time.Now().Round(time.Millisecond).UnixNano() / (int64(time.Millisecond) / int64(time.Nanosecond))
	name := "smoke_test_metric"
	err := monascaclient.CreateMetric(nil, &models.MetricRequestBody{Name:&name, Value:&value, Timestamp:&now})
	if err != nil {
		fmt.Printf("FAILED - Error creating metric %s\n", err.Error())
		return
	}
	fmt.Println("SUCCESS")
	testsSucceeded++
}

func testWebhookTrigger() {
	// Wait 5 minutes for webhook to trigger
	for i := 0; i < 20; i++ {
		if webhookTriggered {
			fmt.Println("SUCCESS")
			testsSucceeded++
		}
		time.Sleep(15 * time.Second)
	}
	fmt.Println("FAILED - Did not recieve webhook")
}

func cleanup(alarmDefinitionID, notificationID string) {
	cleanupStatus := true
	if alarmDefinitionID != ""{
		err := monascaclient.DeleteAlarmDefinition(alarmDefinitionID)
		if err != nil {
			fmt.Printf("FAILED - Error alarm definition - %s\n", err.Error())
			cleanupStatus = false
		}
	}
	if notificationID != "" {
		err := monascaclient.DeleteNotificationMethod(notificationID)
		if err != nil {
			fmt.Printf("FAILED - Error deleting alarm definition - %s\n", err.Error())
			cleanupStatus = false
		}
	}
	if cleanupStatus {
		testsSucceeded++
		fmt.Println("SUCCESS")
	}
}

func main() {
	// Initialize keystone client and get Monasca endpoint
	keystoneOpts, err := openstack.AuthOptionsFromEnv()

	if err != nil {
		fmt.Printf("ERROR setting up keystone client - %s", err.Error())
		os.Exit(1)
	}

	monascaURL := os.Getenv("MONASCA_URL")

	if monascaURL == "" {
		fmt.Println("Monasca URL environment variable must be set")
		os.Exit(1)
	}

	keystoneToken, err := getToken(&keystoneOpts)

	if err != nil {
		fmt.Printf("ERROR getting keystone token - %s", err.Error())
		os.Exit(1)
	}

	initializeMonascaClient(keystoneToken, monascaURL)

	fmt.Println("TEST MEASUREMENTS FLOWING")
	testMeasurementsFlowing()
	fmt.Println()

	// Set Up Webhook server for notifications
	webhookIP := os.Getenv("WEBHOOK_IP")
	if webhookIP == "" {
		webhookIP = "127.0.0.1"
	}
	webhookAddress := "http://" + webhookIP + ":8080"
	http.HandleFunc("/", handleWebhook)
	go http.ListenAndServe(webhookAddress, nil)

	fmt.Println("TEST NOTIFICATION CREATION")
	notificationID := testCreateNotification(webhookAddress)
	fmt.Println()

	fmt.Println("TEST ALARM DEFINITION CREATION")
	alarmDefinitionID := testCreateAlarmDefinition(notificationID)
	fmt.Println()

	fmt.Println("TEST METRIC CREATION")
	testCreateMetric(1)
	fmt.Println()

	fmt.Println("TEST WEBHOOK TRIGGERED")
	testWebhookTrigger()
	fmt.Println()

	fmt.Println("TEST CLEANUP")
	cleanup(alarmDefinitionID, notificationID)
	fmt.Println()

	if testsSucceeded != totalTests {
		fmt.Printf("Smoke Tests Failed. %d/%d passed\n", testsSucceeded, totalTests)
		os.Exit(1)
	} else{
		fmt.Println("All smoke tests passed successfully!!!")
	}
}