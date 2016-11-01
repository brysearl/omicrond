package main

import (
  "time"
  "flag"
  "github.com/Sirupsen/logrus"
  "github.com/brysearl/omicrond/conf"
  "github.com/brysearl/omicrond/job"
  "github.com/brysearl/omicrond/api"
)
//"github.com/davecgh/go-spew/spew"


var runningChanComm chan api.ChanComm
var isUnitTest bool

func init() {

  // Configure command line arguments
  var logLevelPtr = flag.Int("v", conf.Attr.LogLevel, "Set the debug level: 1 = Panic, 2 = Fatal, 3 = Error, 4 = Warn, 5 = Info, 6 = Debug")
  var jobConfigPathPtr = flag.String("config", conf.Attr.JobConfigPath, "Path to the daemon configuration file")
  var apiAddressPtr = flag.String("api_address", conf.Attr.APIAddress, "IP to run the API service")
  var apiPortPtr = flag.Int("api_port", conf.Attr.APIPort, "Port to run the API service")
  var apiTimeoutPtr = flag.Int("api_timeout", conf.Attr.APITimeout, "API service request timeout in seconds")

  // Retrieve command line arguments
  flag.Parse()

  // Set the path to the daemon config file
  conf.Attr.JobConfigPath = *jobConfigPathPtr

  // Set the log level of the program
  conf.Attr.LogLevel = *logLevelPtr

  // Set the address of the api service
  conf.Attr.APIAddress = *apiAddressPtr

  // Set the port of the api service
  conf.Attr.APIPort = *apiPortPtr

  // Set the port of the api service
  conf.Attr.APITimeout = *apiTimeoutPtr

  switch {
  case conf.Attr.LogLevel == 1:
    logrus.SetLevel(logrus.PanicLevel)
  case conf.Attr.LogLevel == 2:
    logrus.SetLevel(logrus.FatalLevel)
  case conf.Attr.LogLevel == 3:
    logrus.SetLevel(logrus.ErrorLevel)
  case conf.Attr.LogLevel == 4:
    logrus.SetLevel(logrus.WarnLevel)
  case conf.Attr.LogLevel == 5:
    logrus.SetLevel(logrus.InfoLevel)
  case conf.Attr.LogLevel == 6:
    logrus.SetLevel(logrus.DebugLevel)
  default:
    logrus.SetLevel(logrus.InfoLevel)
  }

  // Output with absolute time
  customFormatter := new(logrus.TextFormatter)
  customFormatter.TimestampFormat = "2006-01-02 15:04:05"
  logrus.SetFormatter(customFormatter)
  customFormatter.FullTimestamp = true
}

func main() {

  logrus.Info("Starting")

  logrus.Info("Reading job configuration file: " + conf.Attr.JobConfigPath)
  var schedule job.JobSchedule
  err := schedule.ParseJobConfig(conf.Attr.JobConfigPath)
  if err != nil {
    logrus.Fatal(err)
  }

  logrus.Info("Starting API")
  runningChanComm = make(chan api.ChanComm)
  go api.StartServer(runningChanComm)
  time.Sleep(time.Second)

  logrus.Info("Starting scheduling loop")
  startSchedulingLoop(schedule, conf.Attr.JobConfigPath)
}

// startSchedulingLoop - Endless loop that checks jobs every minute and executes them if scheduled
func startSchedulingLoop(schedule job.JobSchedule, jobConfig string) {

  // Keep track of the last minute that was run.  This way we can sit quietly until the next minute comes.
  lastCheckTime := time.Now().Truncate(time.Minute)

  // Keep track of running jobs
  Running := job.RunningJobTracker{}
  Running.Jobs = make(map[string]job.RunningJob)

  // To infinity, and beyond
  for {

    // Get the current minute with the seconds rounded down
    currentTime := time.Now().Truncate(time.Minute)

    // Wait patiently for a new minute
    if currentTime != lastCheckTime {

      //Check each configured job to see if it needs to be run in this minute
      logrus.Info("Running filters: " + currentTime.String())
      for jobIndex, _ := range schedule.Job {

        logrus.Debug("Checking: " + schedule.Job[jobIndex].Label)
        runJob := schedule.Job[jobIndex].CheckIfScheduled(currentTime)

        if runJob == true {

          // Check to see if its running and skip if locking attribute enabled
          if schedule.Job[jobIndex].Locking == true {
            var skip bool
            Running.RLock()
            for runToken, _ := range Running.Jobs {
              if schedule.Job[jobIndex].Label == Running.Jobs[runToken].Config.Label {
                skip = true
                break
              }
            }
            Running.RUnlock()

            if skip {
              logrus.Info(schedule.Job[jobIndex].Label + " running and locked.  Skipping.")
              continue
            }
          }

          // Prep the Job for Running and create a tracking token
          runToken := job.CreateRunToken()
          newJob := job.RunningJob{
            Token: runToken,
            Config: schedule.Job[jobIndex],
            Channel: make(chan string)}

          // Add the tracking token to the tracker
          Running.Lock()
          Running.Jobs[runToken] = newJob
          Running.Unlock()

          // Split off the job into a goroutine
          go func(Running job.RunningJobTracker, newJob job.RunningJob, runToken string) {

            logrus.Info("Adding job " + runToken)
            // Start the job
            newJob.Run()

            // On completion, remove the tracking token from the tracker
            Running.RLock()
            _, ok := Running.Jobs[runToken]
            Running.RUnlock()

            if ok {
              logrus.Info("Removing job " + runToken)
              Running.Lock()
              delete(Running.Jobs, runToken)
              Running.Unlock()
            } else {
              logrus.Error("Could not find runToken on completion")
            }
          }(Running, newJob, runToken)
        }
      }

      // Update the minute lock and take a break
      lastCheckTime = currentTime

    } else {

      // Determine the amount of free time available to listen to a channel
      timeout := time.Now().Truncate(time.Minute).Add(time.Minute).Sub(time.Now())
      logrus.Debug("Listening to channel for the next " + timeout.String() + " seconds")

      // Between scheduling, be open to schedule changes via API
      stop := false
      for stop == false {
        select {

        // Timeout a second before the next minute and break out of channel loop
        case <-time.After(timeout):
          logrus.Debug("No longer listing on channel")
          stop = true

        // Spawn thread on channel traffic and go back to listening
        case incomingChanComm := <-runningChanComm:

        // Spawn thread so we can get back to listening
          go func() {
            // Send the running schedule to a requestor over the same channel
            if incomingChanComm.Signal == "getRunningSchedule" {
              runningChanComm <- api.ChanComm{Handler: schedule, Signal: "getRunningSchedule"}

            } else if incomingChanComm.Signal == "replaceRunningSchedule" {
              // Replace the running schedule with that of the requestor
              err := incomingChanComm.Handler.CheckConfig()
              if err != nil {
                logrus.Error(err)
              }

              logrus.Debug("Schedule Refreshed")
              if isUnitTest != true {
                incomingChanComm.Handler.WriteJobConfig(jobConfig)
              }
              schedule = incomingChanComm.Handler
            } else if incomingChanComm.Signal == "shutdown" {
              logrus.Info("Recieved shutdown command.  Goodbye...")
              return
            }
          }()
        }
      }
    }
  }
}