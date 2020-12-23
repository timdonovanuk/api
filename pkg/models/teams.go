// Vikunja is a to-do list application to facilitate your life.
// Copyright 2018-2020 Vikunja and contributors. All rights reserved.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package models

import (
	"time"

	"xorm.io/xorm"

	"code.vikunja.io/api/pkg/metrics"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/web"
	"xorm.io/builder"
)

// Team holds a team object
type Team struct {
	// The unique, numeric id of this team.
	ID int64 `xorm:"bigint autoincr not null unique pk" json:"id" param:"team"`
	// The name of this team.
	Name string `xorm:"varchar(250) not null" json:"name" valid:"required,runelength(1|250)" minLength:"1" maxLength:"250"`
	// The team's description.
	Description string `xorm:"longtext null" json:"description"`
	CreatedByID int64  `xorm:"bigint not null INDEX" json:"-"`

	// The user who created this team.
	CreatedBy *user.User `xorm:"-" json:"created_by"`
	// An array of all members in this team.
	Members []*TeamUser `xorm:"-" json:"members"`

	// A timestamp when this relation was created. You cannot change this value.
	Created time.Time `xorm:"created" json:"created"`
	// A timestamp when this relation was last updated. You cannot change this value.
	Updated time.Time `xorm:"updated" json:"updated"`

	web.CRUDable `xorm:"-" json:"-"`
	web.Rights   `xorm:"-" json:"-"`
}

// TableName makes beautiful table names
func (Team) TableName() string {
	return "teams"
}

// TeamMember defines the relationship between a user and a team
type TeamMember struct {
	// The unique, numeric id of this team member relation.
	ID int64 `xorm:"bigint autoincr not null unique pk" json:"id"`
	// The team id.
	TeamID int64 `xorm:"bigint not null INDEX" json:"-" param:"team"`
	// The username of the member. We use this to prevent automated user id entering.
	Username string `xorm:"-" json:"username" param:"user"`
	// Used under the hood to manage team members
	UserID int64 `xorm:"bigint not null INDEX" json:"-"`
	// Whether or not the member is an admin of the team. See the docs for more about what a team admin can do
	Admin bool `xorm:"null" json:"admin"`

	// A timestamp when this relation was created. You cannot change this value.
	Created time.Time `xorm:"created not null" json:"created"`

	web.CRUDable `xorm:"-" json:"-"`
	web.Rights   `xorm:"-" json:"-"`
}

// TableName makes beautiful table names
func (TeamMember) TableName() string {
	return "team_members"
}

// TeamUser is the team member type
type TeamUser struct {
	user.User `xorm:"extends"`
	// Whether or not the member is an admin of the team. See the docs for more about what a team admin can do
	Admin  bool  `json:"admin"`
	TeamID int64 `json:"-"`
}

// GetTeamByID gets a team by its ID
func GetTeamByID(s *xorm.Session, id int64) (team *Team, err error) {
	if id < 1 {
		return team, ErrTeamDoesNotExist{id}
	}

	t := Team{}

	exists, err := s.
		Where("id = ?", id).
		Get(&t)
	if err != nil {
		return
	}
	if !exists {
		return &t, ErrTeamDoesNotExist{id}
	}

	teamSlice := []*Team{&t}
	err = addMoreInfoToTeams(s, teamSlice)
	if err != nil {
		return
	}

	team = &t

	return
}

func addMoreInfoToTeams(s *xorm.Session, teams []*Team) (err error) {
	// Put the teams in a map to make assigning more info to it more efficient
	teamMap := make(map[int64]*Team, len(teams))
	var teamIDs []int64
	var ownerIDs []int64
	for _, team := range teams {
		teamMap[team.ID] = team
		teamIDs = append(teamIDs, team.ID)
		ownerIDs = append(ownerIDs, team.CreatedByID)
	}

	// Get all owners and team members
	users := make(map[int64]*TeamUser)
	err = s.
		Select("*").
		Table("users").
		Join("LEFT", "team_members", "team_members.user_id = users.id").
		Join("LEFT", "teams", "team_members.team_id = teams.id").
		Or(
			builder.In("team_id", teamIDs),
			builder.And(
				builder.In("users.id", ownerIDs),
				builder.Expr("teams.created_by_id = users.id"),
				builder.In("teams.id", teamIDs),
			),
		).
		Find(&users)
	if err != nil {
		return
	}
	for _, u := range users {
		if _, exists := teamMap[u.TeamID]; !exists {
			continue
		}
		u.Email = ""
		teamMap[u.TeamID].Members = append(teamMap[u.TeamID].Members, u)
	}

	// We need to do this in a second loop as owners might not be the last ones in the list
	for _, team := range teamMap {
		if teamUser, has := users[team.CreatedByID]; has {
			team.CreatedBy = &teamUser.User
		}
	}
	return
}

// ReadOne implements the CRUD method to get one team
// @Summary Gets one team
// @Description Returns a team by its ID.
// @tags team
// @Accept json
// @Produce json
// @Security JWTKeyAuth
// @Param id path int true "Team ID"
// @Success 200 {object} models.Team "The team"
// @Failure 403 {object} web.HTTPError "The user does not have access to the team"
// @Failure 500 {object} models.Message "Internal error"
// @Router /teams/{id} [get]
func (t *Team) ReadOne(s *xorm.Session) (err error) {
	team, err := GetTeamByID(s, t.ID)
	if team != nil {
		*t = *team
	}
	return
}

// ReadAll gets all teams the user is part of
// @Summary Get teams
// @Description Returns all teams the current user is part of.
// @tags team
// @Accept json
// @Produce json
// @Param page query int false "The page number. Used for pagination. If not provided, the first page of results is returned."
// @Param per_page query int false "The maximum number of items per page. Note this parameter is limited by the configured maximum of items per page."
// @Param s query string false "Search teams by its name."
// @Security JWTKeyAuth
// @Success 200 {array} models.Team "The teams."
// @Failure 500 {object} models.Message "Internal error"
// @Router /teams [get]
func (t *Team) ReadAll(s *xorm.Session, a web.Auth, search string, page int, perPage int) (result interface{}, resultCount int, numberOfTotalItems int64, err error) {
	if _, is := a.(*LinkSharing); is {
		return nil, 0, 0, ErrGenericForbidden{}
	}

	limit, start := getLimitFromPageIndex(page, perPage)

	all := []*Team{}
	query := s.Select("teams.*").
		Table("teams").
		Join("INNER", "team_members", "team_members.team_id = teams.id").
		Where("team_members.user_id = ?", a.GetID()).
		Where("teams.name LIKE ?", "%"+search+"%")
	if limit > 0 {
		query = query.Limit(limit, start)
	}
	err = query.Find(&all)
	if err != nil {
		return nil, 0, 0, err
	}

	err = addMoreInfoToTeams(s, all)
	if err != nil {
		return nil, 0, 0, err
	}

	numberOfTotalItems, err = s.
		Table("teams").
		Join("INNER", "team_members", "team_members.team_id = teams.id").
		Where("team_members.user_id = ?", a.GetID()).
		Where("teams.name LIKE ?", "%"+search+"%").
		Count(&Team{})
	return all, len(all), numberOfTotalItems, err
}

// Create is the handler to create a team
// @Summary Creates a new team
// @Description Creates a new team in a given namespace. The user needs write-access to the namespace.
// @tags team
// @Accept json
// @Produce json
// @Security JWTKeyAuth
// @Param team body models.Team true "The team you want to create."
// @Success 200 {object} models.Team "The created team."
// @Failure 400 {object} web.HTTPError "Invalid team object provided."
// @Failure 500 {object} models.Message "Internal error"
// @Router /teams [put]
func (t *Team) Create(s *xorm.Session, a web.Auth) (err error) {
	doer, err := user.GetFromAuth(a)
	if err != nil {
		return err
	}

	// Check if we have a name
	if t.Name == "" {
		return ErrTeamNameCannotBeEmpty{}
	}

	t.CreatedByID = doer.ID
	t.CreatedBy = doer

	_, err = s.Insert(t)
	if err != nil {
		return
	}

	// Insert the current user as member and admin
	tm := TeamMember{TeamID: t.ID, Username: doer.Username, Admin: true}
	if err = tm.Create(s, doer); err != nil {
		return err
	}

	metrics.UpdateCount(1, metrics.TeamCountKey)
	return
}

// Delete deletes a team
// @Summary Deletes a team
// @Description Delets a team. This will also remove the access for all users in that team.
// @tags team
// @Produce json
// @Security JWTKeyAuth
// @Param id path int true "Team ID"
// @Success 200 {object} models.Message "The team was successfully deleted."
// @Failure 400 {object} web.HTTPError "Invalid team object provided."
// @Failure 500 {object} models.Message "Internal error"
// @Router /teams/{id} [delete]
func (t *Team) Delete(s *xorm.Session) (err error) {

	// Delete the team
	_, err = s.ID(t.ID).Delete(&Team{})
	if err != nil {
		return
	}

	// Delete team members
	_, err = s.Where("team_id = ?", t.ID).Delete(&TeamMember{})
	if err != nil {
		return
	}

	// Delete team <-> namespace relations
	_, err = s.Where("team_id = ?", t.ID).Delete(&TeamNamespace{})
	if err != nil {
		return
	}

	// Delete team <-> lists relations
	_, err = s.Where("team_id = ?", t.ID).Delete(&TeamList{})
	if err != nil {
		return
	}

	metrics.UpdateCount(-1, metrics.TeamCountKey)
	return
}

// Update is the handler to create a team
// @Summary Updates a team
// @Description Updates a team.
// @tags team
// @Accept json
// @Produce json
// @Security JWTKeyAuth
// @Param id path int true "Team ID"
// @Param team body models.Team true "The team with updated values you want to update."
// @Success 200 {object} models.Team "The updated team."
// @Failure 400 {object} web.HTTPError "Invalid team object provided."
// @Failure 500 {object} models.Message "Internal error"
// @Router /teams/{id} [post]
func (t *Team) Update(s *xorm.Session) (err error) {
	// Check if we have a name
	if t.Name == "" {
		return ErrTeamNameCannotBeEmpty{}
	}

	// Check if the team exists
	_, err = GetTeamByID(s, t.ID)
	if err != nil {
		return
	}

	_, err = s.ID(t.ID).Update(t)
	if err != nil {
		return
	}

	// Get the newly updated team
	team, err := GetTeamByID(s, t.ID)
	if team != nil {
		*t = *team
	}

	return
}
