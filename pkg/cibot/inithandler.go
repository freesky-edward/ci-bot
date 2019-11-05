package cibot

import (
	"context"
	"encoding/base64"
	"time"

	"gitee.com/openeuler/ci-bot/pkg/cibot/config"
	"gitee.com/openeuler/ci-bot/pkg/cibot/database"
	"gitee.com/openeuler/go-gitee/gitee"
	"github.com/antihax/optional"
	"github.com/golang/glog"
	"gopkg.in/yaml.v2"
)

type InitHandler struct {
	Config       config.Config
	Context      context.Context
	GiteeClient  *gitee.APIClient
	ProjectsFile string
}

type Projects struct {
	Community    Community    `yaml:"community"`
	Repositories []Repository `yaml:"repositories"`
}

type Community struct {
	Name       *string  `yaml:"name"`
	Managers   []string `yaml:"managers"`
	Developers []string `yaml:"developers"`
	Viewers    []string `yaml:"viewers"`
	Reporters  []string `yaml:"reporters"`
}

type Repository struct {
	Name        *string  `yaml:"name"`
	Description *string  `yaml:"description"`
	Type        *string  `yaml:"type"`
	Managers    []string `yaml:"managers"`
	Developers  []string `yaml:"developers"`
	Viewers     []string `yaml:"viewers"`
	Reporters   []string `yaml:"reporters"`
}

var (
	PrivilegeManager   = "manager"
	PrivilegeDeveloper = "developer"
	PrivilegeViewer    = "viewer"
	PrivilegeReporter  = "reporter"
)

// Serve
func (handler *InitHandler) Serve() {
	// init waiting sha
	err := handler.initWaitingSha()
	if err != nil {
		glog.Errorf("unable to initWaitingSha: %v", err)
		return
	}
	// watch database
	handler.watch()
}

// initWaitingSha init waiting sha
func (handler *InitHandler) initWaitingSha() error {
	// get params
	watchOwner := handler.Config.WatchProjectFileOwner
	watchRepo := handler.Config.WatchprojectFileRepo
	watchPath := handler.Config.WatchprojectFilePath
	watchRef := handler.Config.WatchProjectFileRef

	// invoke api to get file contents
	localVarOptionals := &gitee.GetV5ReposOwnerRepoContentsPathOpts{}
	localVarOptionals.AccessToken = optional.NewString(handler.Config.GiteeToken)
	localVarOptionals.Ref = optional.NewString(watchRef)

	// get contents
	contents, _, err := handler.GiteeClient.RepositoriesApi.GetV5ReposOwnerRepoContentsPath(
		handler.Context, watchOwner, watchRepo, watchPath, localVarOptionals)
	if err != nil {
		glog.Errorf("unable to get repository content: %v", err)
		return err
	}
	// Check project file
	var lenProjectFiles int
	err = database.DBConnection.Model(&database.ProjectFiles{}).
		Where("owner = ? and repo = ? and path = ? and ref = ?", watchOwner, watchRepo, watchPath, watchRef).
		Count(&lenProjectFiles).Error
	if err != nil {
		glog.Errorf("unable to get project files: %v", err)
		return err
	}
	if lenProjectFiles > 0 {
		glog.Infof("project file is exist: %s", contents.Sha)
		// Check sha in database
		updatepf := database.ProjectFiles{}
		err = database.DBConnection.
			Where("owner = ? and repo = ? and path = ? and ref = ?", watchOwner, watchRepo, watchPath, watchRef).
			First(&updatepf).Error
		if err != nil {
			glog.Errorf("unable to get project files: %v", err)
			return err
		}
		// write sha in waitingsha
		updatepf.WaitingSha = contents.Sha
		err = database.DBConnection.Save(&updatepf).Error
		if err != nil {
			glog.Errorf("unable to get project files: %v", err)
			return err
		}

	} else {
		glog.Infof("project file is non-exist: %s", contents.Sha)
		// add project file
		addpf := database.ProjectFiles{
			Owner:      watchOwner,
			Repo:       watchRepo,
			Path:       watchPath,
			Ref:        watchRef,
			WaitingSha: contents.Sha,
		}

		// create project file
		err = database.DBConnection.Create(&addpf).Error
		if err != nil {
			glog.Errorf("unable to create project files: %v", err)
			return err
		}
		glog.Infof("add project file successfully: %s", contents.Sha)
	}
	return nil
}

// watch database
func (handler *InitHandler) watch() {
	// get params
	watchOwner := handler.Config.WatchProjectFileOwner
	watchRepo := handler.Config.WatchprojectFileRepo
	watchPath := handler.Config.WatchprojectFilePath
	watchRef := handler.Config.WatchProjectFileRef
	watchDuration := handler.Config.WatchProjectFileDuration

	for {
		glog.Infof("begin to serve. watchOwner: %s watchRepo: %s watchPath: %s watchRef: %s watchDuration: %d",
			watchOwner, watchRepo, watchPath, watchRef, watchDuration)

		// get project file
		pf := database.ProjectFiles{}
		err := database.DBConnection.
			Where("owner = ? and repo = ? and path = ? and ref = ?", watchOwner, watchRepo, watchPath, watchRef).
			First(&pf).Error
		if err != nil {
			glog.Errorf("unable to get project files: %v", err)
		} else {
			glog.Infof("init handler current sha: %v target sha: %v waiting sha: %v",
				pf.CurrentSha, pf.TargetSha, pf.WaitingSha)
			if pf.TargetSha != "" {
				// skip when there is executing target sha
				glog.Infof("there is executing target sha: %v", pf.TargetSha)
			} else {
				if pf.WaitingSha != "" && pf.CurrentSha != pf.WaitingSha {
					// waiting -> target
					pf.TargetSha = pf.WaitingSha
					err = database.DBConnection.Save(&pf).Error
					if err != nil {
						glog.Errorf("unable to save project files: %v", err)
					} else {
						// get file content from target sha
						glog.Infof("get target sha blob: %v", pf.TargetSha)
						localVarOptionals := &gitee.GetV5ReposOwnerRepoGitBlobsShaOpts{}
						localVarOptionals.AccessToken = optional.NewString(handler.Config.GiteeToken)
						blob, _, err := handler.GiteeClient.GitDataApi.GetV5ReposOwnerRepoGitBlobsSha(
							handler.Context, watchOwner, watchRepo, pf.TargetSha, localVarOptionals)
						if err != nil {
							glog.Errorf("unable to get blob: %v", err)
						} else {
							// base64 decode
							glog.Infof("decode target sha blob: %v", pf.TargetSha)
							decodeBytes, err := base64.StdEncoding.DecodeString(blob.Content)
							if err != nil {
								glog.Errorf("decode content with error: %v", err)
							} else {
								// unmarshal owners file
								glog.Infof("unmarshal target sha blob: %v", pf.TargetSha)
								var ps Projects
								err = yaml.Unmarshal(decodeBytes, &ps)
								if err != nil {
									glog.Errorf("failed to unmarshal projects: %v", err)
								} else {
									glog.Infof("get blob result: %v", ps)
									for i := 0; i < len(ps.Repositories); i++ {
										// get repositories length
										lenRepositories, err := handler.getRepositoriesLength(*ps.Community.Name, *ps.Repositories[i].Name, pf.ID)
										if err != nil {
											glog.Errorf("failed to get repositories length: %v", err)
											continue
										}
										if lenRepositories > 0 {
											glog.Infof("repository: %s is exist. no action.", *ps.Repositories[i].Name)
										} else {
											// add repository
											err = handler.addRepositories(*ps.Community.Name, *ps.Repositories[i].Name,
												*ps.Repositories[i].Description, *ps.Repositories[i].Type, pf.ID)
											if err != nil {
												glog.Errorf("failed to add repositories: %v", err)
											}
										}
										// add members
										err = handler.handleMembers(ps.Community, ps.Repositories[i])
										if err != nil {
											glog.Errorf("failed to add members: %v", err)
										}
									}
								}
							}
						}
					}
				} else {
					glog.Infof("no waiting sha: %v", pf.WaitingSha)
				}
			}
		}

		// watch duration
		glog.Info("end to serve")
		time.Sleep(time.Duration(watchDuration) * time.Second)
	}
}

// GetRepositoriesLength get repositories length
func (handler *InitHandler) getRepositoriesLength(owner string, repo string, id uint) (int, error) {
	// Check repositories file
	var lenRepositories int
	err := database.DBConnection.Model(&database.Repositories{}).
		Where("owner = ? and repo = ? and project_file_id = ?", owner, repo, id).
		Count(&lenRepositories).Error
	if err != nil {
		glog.Errorf("unable to get repositories files: %v", err)
	}
	return lenRepositories, err
}

// addRepositories add repository
func (handler *InitHandler) addRepositories(owner, repo, description, t string, id uint) error {
	// add repository in gitee
	err := handler.addRepositoriesinGitee(owner, repo, description, t)
	if err != nil {
		glog.Errorf("failed to add repositories: %v", err)
		return err
	}

	// add repository in database
	err = handler.addRepositoriesinDB(owner, repo, description, t, id)
	if err != nil {
		glog.Errorf("failed to add repositories: %v", err)
		return err
	}
	return nil
}

// addRepositoriesinDB add repository in database
func (handler *InitHandler) addRepositoriesinDB(owner, repo, description, t string, id uint) error {
	// add repository
	addrepo := database.Repositories{
		Owner:         owner,
		Repo:          repo,
		Description:   description,
		Type:          t,
		ProjectFileID: id,
	}

	// create repository
	err := database.DBConnection.Create(&addrepo).Error
	if err != nil {
		glog.Errorf("unable to create repository: %v", err)
		return err
	}
	return nil
}

// addRepositoriesinGitee add repository in giteee
func (handler *InitHandler) addRepositoriesinGitee(owner, repo, description, t string) error {
	// build create repository param
	repobody := gitee.RepositoryPostParam{}
	repobody.AccessToken = handler.Config.GiteeToken
	repobody.Name = repo
	repobody.Description = description
	repobody.HasIssues = true
	repobody.HasWiki = true
	if t == "private" {
		repobody.Private = true
	} else {
		repobody.Private = false
	}

	// invoke create repository
	glog.Infof("begin to create repository: %s", repo)
	_, _, err := handler.GiteeClient.RepositoriesApi.PostV5OrgsOrgRepos(handler.Context, owner, repobody)
	if err != nil {
		glog.Errorf("fail to create repository: %v", err)
		return err
	}
	glog.Infof("end to create repository: %s", repo)
	return nil
}

// isUsingRepositoryMember check if using repository member not community member
func (handler *InitHandler) isUsingRepositoryMember(r Repository) bool {
	return len(r.Managers) > 0 || len(r.Developers) > 0 || len(r.Viewers) > 0 || len(r.Reporters) > 0
}

// handleMembers handle members
func (handler *InitHandler) handleMembers(c Community, r Repository) error {
	// if the members is defined in the repositories, it means that
	// all the members defined in the community will not be inherited by repositories.
	members := make(map[string]map[string]string)
	if handler.isUsingRepositoryMember(r) {
		// using repositories members
		members = handler.getMembersMap(r.Managers, r.Developers, r.Viewers, r.Reporters)

	} else {
		// using community members
		members = handler.getMembersMap(c.Managers, c.Developers, c.Viewers, c.Reporters)
	}

	// get members from database
	var ps []database.Privileges
	err := database.DBConnection.Model(&database.Privileges{}).
		Where("owner = ? and repo = ?", c.Name, r.Name).Find(&ps).Error
	if err != nil {
		glog.Errorf("unable to get members: %v", err)
		return err
	}
	membersinDB := handler.getMembersMapByDB(ps)

	// managers
	handler.addManagers(members[PrivilegeManager], membersinDB[PrivilegeManager])
	handler.removeManagers(members[PrivilegeManager], membersinDB[PrivilegeManager])

	// developers
	handler.addDevelopers(members[PrivilegeDeveloper], membersinDB[PrivilegeDeveloper])
	handler.removeDevelopers(members[PrivilegeDeveloper], membersinDB[PrivilegeDeveloper])

	// viewers
	handler.addViewers(members[PrivilegeViewer], membersinDB[PrivilegeViewer])
	handler.removeViewers(members[PrivilegeViewer], membersinDB[PrivilegeViewer])

	// reporters
	handler.addReporters(members[PrivilegeReporter], membersinDB[PrivilegeReporter])
	handler.removeReporters(members[PrivilegeReporter], membersinDB[PrivilegeReporter])

	return nil
}

// getMembersMap get members map
func (handler *InitHandler) getMembersMap(managers, developers, viewers, reporters []string) map[string]map[string]string {
	mapManagers := make(map[string]string)
	mapDevelopers := make(map[string]string)
	mapViewers := make(map[string]string)
	mapReporters := make(map[string]string)
	if len(managers) > 0 {
		for _, m := range managers {
			mapManagers[m] = m
		}
	}
	if len(developers) > 0 {
		for _, d := range developers {
			// skip when developer is already in managers
			_, okinManagers := mapManagers[d]
			if !okinManagers {
				mapDevelopers[d] = d
			}
		}
	}
	if len(viewers) > 0 {
		for _, v := range viewers {
			// skip when viewer is already in managers or developers
			_, okinManagers := mapManagers[v]
			_, okinDevelopers := mapDevelopers[v]
			if !okinManagers && !okinDevelopers {
				mapViewers[v] = v
			}
		}
	}
	if len(reporters) > 0 {
		for _, rt := range reporters {
			// skip when reporter is already in managers or developers or viewer
			_, okinManagers := mapManagers[rt]
			_, okinDevelopers := mapDevelopers[rt]
			_, okinViewers := mapViewers[rt]
			if !okinManagers && !okinDevelopers && !okinViewers {
				mapReporters[rt] = rt
			}
		}
	}

	// all members map
	members := make(map[string]map[string]string)
	members[PrivilegeManager] = mapManagers
	members[PrivilegeDeveloper] = mapDevelopers
	members[PrivilegeViewer] = mapViewers
	members[PrivilegeReporter] = mapReporters
	return members
}

// getMembersMapByDB get members map from database
func (handler *InitHandler) getMembersMapByDB(ps []database.Privileges) map[string]map[string]string {
	members := make(map[string]map[string]string)
	mapManagers := make(map[string]string)
	mapDevelopers := make(map[string]string)
	mapViewers := make(map[string]string)
	mapReporters := make(map[string]string)
	// all members map
	members[PrivilegeManager] = mapManagers
	members[PrivilegeDeveloper] = mapDevelopers
	members[PrivilegeViewer] = mapViewers
	members[PrivilegeReporter] = mapReporters
	if len(ps) > 0 {
		for _, p := range ps {
			members[p.Type][p.User] = p.User
		}
	}

	return members
}

// addManagers add managers
func (handler *InitHandler) addManagers(mapManagers, mapManagersInDB map[string]string) error {
	// managers added
	listOfAddManagers := make([]string, 0)
	for _, m := range mapManagers {
		_, okinManagers := mapManagersInDB[m]
		if !okinManagers {
			listOfAddManagers = append(listOfAddManagers, m)
		}
	}
	glog.Infof("list of add managers: %v", listOfAddManagers)
	return nil
}

// addDevelopers add developers
func (handler *InitHandler) addDevelopers(mapDevelopers, mapDevelopersInDB map[string]string) error {
	// developers added
	listOfAddDevelopers := make([]string, 0)
	for _, d := range mapDevelopers {
		_, okinDevelopers := mapDevelopersInDB[d]
		if !okinDevelopers {
			listOfAddDevelopers = append(listOfAddDevelopers, d)
		}
	}
	glog.Infof("list of add developers: %v", listOfAddDevelopers)
	return nil
}

// addViewers add viewers
func (handler *InitHandler) addViewers(mapViewers, mapViewersInDB map[string]string) error {
	// viewers added
	listOfAddViewers := make([]string, 0)
	for _, v := range mapViewers {
		_, okinViewers := mapViewersInDB[v]
		if !okinViewers {
			listOfAddViewers = append(listOfAddViewers, v)
		}
	}
	glog.Infof("list of add viewers: %v", listOfAddViewers)
	return nil
}

// addReporters add reporters
func (handler *InitHandler) addReporters(mapReporters, mapReportersInDB map[string]string) error {
	// reporters added
	listOfAddReporters := make([]string, 0)
	for _, rt := range mapReporters {
		_, okinReporters := mapReportersInDB[rt]
		if !okinReporters {
			listOfAddReporters = append(listOfAddReporters, rt)
		}
	}
	glog.Infof("list of add reporters: %v", listOfAddReporters)
	return nil
}

// removeManagers remove managers
func (handler *InitHandler) removeManagers(mapManagers, mapManagersInDB map[string]string) error {
	// managers removed
	listOfRemoveManagers := make([]string, 0)
	for _, m := range mapManagersInDB {
		_, okinManagers := mapManagers[m]
		if !okinManagers {
			listOfRemoveManagers = append(listOfRemoveManagers, m)
		}
	}
	glog.Infof("list of removed managers: %v", listOfRemoveManagers)
	return nil
}

// removeDevelopers remove developers
func (handler *InitHandler) removeDevelopers(mapDevelopers, mapDevelopersInDB map[string]string) error {
	// developers removed
	listOfRemoveDevelopers := make([]string, 0)
	for _, d := range mapDevelopersInDB {
		_, okinDevelopers := mapDevelopers[d]
		if !okinDevelopers {
			listOfRemoveDevelopers = append(listOfRemoveDevelopers, d)
		}
	}
	glog.Infof("list of removed developers: %v", listOfRemoveDevelopers)
	return nil
}

// removeViewers remove viewers
func (handler *InitHandler) removeViewers(mapViewers, mapViewersInDB map[string]string) error {
	// viewers removed
	listOfRemoveViewers := make([]string, 0)
	for _, v := range mapViewersInDB {
		_, okinViewers := mapViewers[v]
		if !okinViewers {
			listOfRemoveViewers = append(listOfRemoveViewers, v)
		}
	}
	glog.Infof("list of removed viewers: %v", listOfRemoveViewers)
	return nil
}

// removeReporters remove reporters
func (handler *InitHandler) removeReporters(mapReporters, mapReportersInDB map[string]string) error {
	// reporters removed
	listOfRemoveReporters := make([]string, 0)
	for _, rt := range mapReportersInDB {
		_, okinReporters := mapReporters[rt]
		if !okinReporters {
			listOfRemoveReporters = append(listOfRemoveReporters, rt)
		}
	}
	glog.Infof("list of removed reporters: %v", listOfRemoveReporters)
	return nil
}
