/*
 * 码云 Open API
 *
 * No description provided (generated by Swagger Codegen https://github.com/swagger-api/swagger-codegen)
 *
 * API version: 5.3.2
 * Generated by: Swagger Codegen (https://github.com/swagger-api/swagger-codegen.git)
 */

package gitee

// 获取一个组织
type Group struct {
	Id          int32  `json:"id,omitempty"`
	Login       string `json:"login,omitempty"`
	Url         string `json:"url,omitempty"`
	AvatarUrl   string `json:"avatar_url,omitempty"`
	ReposUrl    string `json:"repos_url,omitempty"`
	EventsUrl   string `json:"events_url,omitempty"`
	MembersUrl  string `json:"members_url,omitempty"`
	Description string `json:"description,omitempty"`
}
