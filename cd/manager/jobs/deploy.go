package jobs

import (
	"fmt"
	"log"
	"time"

	"github.com/3box/pipeline-tools/cd/manager"
)

const LayoutParam = "layout"

var _ manager.Job = &deployJob{}

type deployJob struct {
	state       manager.JobState
	db          manager.Database
	d           manager.Deployment
	notifs      manager.Notifs
	component   manager.DeployComponent
	sha         string
	registryUri string
}

func DeployJob(db manager.Database, d manager.Deployment, notifs manager.Notifs, jobState manager.JobState) (*deployJob, error) {
	if component, found := jobState.Params[manager.JobParam_Component].(string); !found {
		return nil, fmt.Errorf("deployJob: missing component (ceramic, ipfs, cas)")
	} else if sha, found := jobState.Params[manager.JobParam_Sha].(string); !found {
		return nil, fmt.Errorf("deployJob: missing sha")
	} else {
		c := manager.DeployComponent(component)
		if clusterLayout, err := d.PopulateLayout(c); err != nil {
			return nil, err
		} else if registryUri, err := d.GetRegistryUri(c); err != nil {
			return nil, err
		} else {
			// Only overwrite the cluster layout if it wasn't already present.
			if _, found = jobState.Params[LayoutParam]; !found {
				jobState.Params[LayoutParam] = clusterLayout
			}
			return &deployJob{jobState, db, d, notifs, c, sha, registryUri}, nil
		}
	}
}

func (d deployJob) AdvanceJob() (manager.JobState, error) {
	if d.state.Stage == manager.JobStage_Queued {
		if err := d.updateCluster(); err != nil {
			d.state.Stage = manager.JobStage_Failed
			d.state.Params[manager.JobParam_Error] = err.Error()
			log.Printf("deployJob: error updating service: %v, %s", err, manager.PrintJob(d.state))
		} else {
			d.state.Stage = manager.JobStage_Started
			// For started deployments update the build commit hash in the DB.
			if err = d.db.UpdateBuildHash(d.component, d.sha); err != nil {
				// This isn't an error big enough to fail the job, just report and move on.
				log.Printf("deployJob: failed to update build hash: %v, %s", err, manager.PrintJob(d.state))
			}
		}
	} else if time.Now().Add(-manager.DefaultFailureTime).After(d.state.Ts) {
		d.state.Stage = manager.JobStage_Failed
		d.state.Params[manager.JobParam_Error] = manager.Error_Timeout
		log.Printf("deployJob: job timed out: %s", manager.PrintJob(d.state))
	} else if d.state.Stage == manager.JobStage_Started {
		// Check if all service updates completed
		if running, err := d.checkCluster(); err != nil {
			d.state.Stage = manager.JobStage_Failed
			d.state.Params[manager.JobParam_Error] = err.Error()
			log.Printf("deployJob: error checking services running status: %v, %s", err, manager.PrintJob(d.state))
		} else if running {
			d.state.Stage = manager.JobStage_Completed
			// For completed deployments update the deploy commit hash in the DB.
			if err = d.db.UpdateDeployHash(d.component, d.sha); err != nil {
				// This isn't an error big enough to fail the job, just report and move on.
				log.Printf("deployJob: failed to update deploy hash: %v, %s", err, manager.PrintJob(d.state))
			}
		} else {
			// Return so we come back again to check
			return d.state, nil
		}
	} else {
		// There's nothing left to do so we shouldn't have reached here
		return d.state, fmt.Errorf("deployJob: unexpected state: %s", manager.PrintJob(d.state))
	}
	// Only send started/completed/failed notifications.
	if (d.state.Stage == manager.JobStage_Started) || (d.state.Stage == manager.JobStage_Failed) || (d.state.Stage == manager.JobStage_Completed) {
		d.notifs.NotifyJob(d.state)
	}
	return d.state, d.db.UpdateJob(d.state)
}

func (d deployJob) updateCluster() error {
	image := d.registryUri + ":" + d.sha
	for cluster, typeLayout := range d.state.Params[LayoutParam].(map[string]interface{}) {
		for deployType, deployLayout := range typeLayout.(map[manager.DeployType]interface{}) {
			switch deployType {
			case manager.DeployType_Service:
				for service, _ := range deployLayout.(map[string]interface{}) {
					if id, err := d.d.UpdateService(cluster, service, image); err != nil {
						return err
					} else {
						deployLayout.(map[string]interface{})[service] = id
					}
				}
			case manager.DeployType_Task:
				for task, _ := range deployLayout.(map[string]interface{}) {
					if id, err := d.d.UpdateTask(task, image); err != nil {
						return err
					} else {
						deployLayout.(map[string]interface{})[task] = id
					}
				}
			default:
				return fmt.Errorf("updateCluster: invalid deploy type: %s", deployType)
			}
		}
	}
	return nil
}

func (d deployJob) checkCluster() (bool, error) {
	// Check the status of cluster services, only return success if all services were successfully started.
	for cluster, typeLayout := range d.state.Params[LayoutParam].(map[string]interface{}) {
		for deployType, deployLayout := range typeLayout.(map[manager.DeployType]interface{}) {
			switch deployType {
			case manager.DeployType_Service:
				for service, id := range deployLayout.(map[string]interface{}) {
					if deployed, err := d.d.CheckService(cluster, service, id.(string)); err != nil {
						return false, err
					} else if !deployed {
						return false, nil
					}
				}
			case manager.DeployType_Task:
			default:
				return false, fmt.Errorf("checkCluster: invalid deploy type: %s", deployType)
			}
		}
	}
	return true, nil
}
