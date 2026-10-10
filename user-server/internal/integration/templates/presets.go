// Package templates 预置 ERP/CRM 对接模板
//
// 五层架构归属: L3 数据模型层（配置数据）
// 设计依据: L 域 缺口修复 - 钉钉/企业微信/飞书/用友/金蝶/管家婆/SAP 字段映射
// 私域独立部署: 无 merchant_id 字段
package templates

import (
	"encoding/json"

	"hivemtk-user/internal/model"
)

const (
	srcCustomerName      = "customer.name"
	srcCustomerPhone     = "customer.phone"
	srcCustomerEmail     = "customer.email"
	srcCustomerCompany   = "customer.company"
	srcCustomerAddress   = "customer.address"
	srcCustomerTags      = "customer.tags"
	srcCustomerUnifiedID = "customer.unified_id"

	srcOrderID    = "order.id"
	srcOrderPrice = "order.price"
	srcOrderTime  = "order.create_time"
)

func dingTalkTemplate() *model.IntegrationTemplate {
	return &model.IntegrationTemplate{
		Code:     "dingtalk_erp_default",
		Platform: model.PlatformDingTalk,
		Category: model.CategoryCRM,
		Name:     "钉钉智能人事/客户对接模板",
		Version:  "1.0.0",
		APIBase:  "https://oapi.dingtalk.com",
		AuthType: model.AuthTypeAPIKey,
		AuthConfig: `{
			"app_key": "",
			"app_secret": "",
			"agent_id": ""
		}`,
		DocURL: "https://open.dingtalk.com/document/",
		FieldMaps: mustJSON([]model.FieldMapping{
			{Source: srcCustomerName, Target: "name", Type: "string", Required: true},
			{Source: srcCustomerPhone, Target: "mobile", Type: "string", Required: true},
			{Source: srcCustomerEmail, Target: "email", Type: "string"},
			{Source: srcCustomerCompany, Target: "company_name", Type: "string"},
			{Source: srcCustomerAddress, Target: "address", Type: "string"},
			{Source: srcCustomerTags, Target: "labels", Type: "string", Transform: "join|,"},
		}),
		Endpoints: mustJSON([]model.EndpointConfig{
			{Name: "获取 access_token", Method: "POST", Path: "/gettoken", Description: "获取企业 access_token"},
			{Name: "创建客户", Method: "POST", Path: "/topapi/customer/batchCreate", Description: "批量创建客户档案"},
			{Name: "查询客户", Method: "POST", Path: "/topapi/customer/list", Description: "查询客户列表"},
		}),
		BuiltIn: true,
		Enabled: true,
		Remark:  "钉钉企业版默认对接模板（智能人事 + 客户管理）",
	}
}

func weComTemplate() *model.IntegrationTemplate {
	return &model.IntegrationTemplate{
		Code:     "wecom_crm_default",
		Platform: model.PlatformWeCom,
		Category: model.CategoryCRM,
		Name:     "企业微信客户联系对接模板",
		Version:  "1.0.0",
		APIBase:  "https://qyapi.weixin.qq.com/cgi-bin",
		AuthType: model.AuthTypeAPIKey,
		AuthConfig: `{
			"corpid": "",
			"corp_secret": "",
			"agent_id": ""
		}`,
		DocURL: "https://developer.work.weixin.qq.com/document/path/",
		FieldMaps: mustJSON([]model.FieldMapping{
			{Source: srcCustomerUnifiedID, Target: "external_userid", Type: "string", Required: true},
			{Source: srcCustomerName, Target: "name", Type: "string", Required: true},
			{Source: srcCustomerPhone, Target: "mobile", Type: "string"},
			{Source: srcCustomerEmail, Target: "email", Type: "string"},
			{Source: srcCustomerCompany, Target: "corp_name", Type: "string"},
			{Source: srcCustomerTags, Target: "tag_id_list", Type: "string", Transform: "wechat_tags"},
		}),
		Endpoints: mustJSON([]model.EndpointConfig{
			{Name: "获取 access_token", Method: "GET", Path: "/gettoken", Description: "获取企业 access_token"},
			{Name: "获取客户列表", Method: "POST", Path: "/externalcontact/list", Description: "获取客户列表"},
			{Name: "获取客户详情", Method: "POST", Path: "/externalcontact/get", Description: "获取客户详情"},
			{Name: "添加客户标签", Method: "POST", Path: "/externalcontact/mark_tag", Description: "为客户打标签"},
		}),
		BuiltIn: true,
		Enabled: true,
		Remark:  "企业微信客户联系/客户标签对接",
	}
}

func feishuTemplate() *model.IntegrationTemplate {
	return &model.IntegrationTemplate{
		Code:     "feishu_crm_default",
		Platform: model.PlatformFeishu,
		Category: model.CategoryCRM,
		Name:     "飞书通讯录/CRM 对接模板",
		Version:  "1.0.0",
		APIBase:  "https://open.feishu.cn/open-apis",
		AuthType: model.AuthTypeAPIKey,
		AuthConfig: `{
			"app_id": "",
			"app_secret": ""
		}`,
		DocURL: "https://open.feishu.cn/document/",
		FieldMaps: mustJSON([]model.FieldMapping{
			{Source: srcCustomerName, Target: "name", Type: "string", Required: true},
			{Source: srcCustomerPhone, Target: "mobile", Type: "string"},
			{Source: srcCustomerEmail, Target: "email", Type: "string"},
			{Source: srcCustomerCompany, Target: "department_ids", Type: "string"},
		}),
		Endpoints: mustJSON([]model.EndpointConfig{
			{Name: "获取 tenant_access_token", Method: "POST", Path: "/auth/v3/tenant_access_token/internal", Description: "获取租户 access_token"},
			{Name: "创建用户", Method: "POST", Path: "/contact/v3/users", Description: "创建用户"},
			{Name: "查询用户", Method: "GET", Path: "/contact/v3/users/{user_id}", Description: "查询用户详情"},
		}),
		BuiltIn: true,
		Enabled: true,
		Remark:  "飞书通讯录 + 自建应用对接",
	}
}

func yonyouTemplate() *model.IntegrationTemplate {
	return &model.IntegrationTemplate{
		Code:     "yonyou_erp_default",
		Platform: model.PlatformYonyou,
		Category: model.CategoryERP,
		Name:     "用友 U8 / YonBIP 财务供应链对接模板",
		Version:  "1.0.0",
		APIBase:  "https://api.yonyoucloud.com",
		AuthType: model.AuthTypeOAuth2,
		AuthConfig: `{
			"app_key": "",
			"app_secret": "",
			"redirect_uri": "",
			"scope": "api"
		}`,
		DocURL: "https://developer.yonyoucloud.com/",
		FieldMaps: mustJSON([]model.FieldMapping{
			{Source: srcCustomerUnifiedID, Target: "customer_code", Type: "string", Required: true},
			{Source: srcCustomerName, Target: "customer_name", Type: "string", Required: true},
			{Source: srcCustomerPhone, Target: "phone", Type: "string"},
			{Source: srcCustomerAddress, Target: "address", Type: "string"},
			{Source: srcCustomerCompany, Target: "company_name", Type: "string"},
			{Source: srcOrderID, Target: "vouch_id", Type: "string"},
			{Source: srcOrderPrice, Target: "amount", Type: "number", Transform: "yuan_to_fen"},
			{Source: srcOrderTime, Target: "vouch_date", Type: "date"},
		}),
		Endpoints: mustJSON([]model.EndpointConfig{
			{Name: "获取 token", Method: "POST", Path: "/oauth/token", Description: "OAuth2 令牌"},
			{Name: "客户档案保存", Method: "POST", Path: "/u8cloud/api/customer/save", Description: "保存客户档案"},
			{Name: "销售订单保存", Method: "POST", Path: "/u8cloud/api/saleorder/save", Description: "保存销售订单"},
			{Name: "应收单查询", Method: "POST", Path: "/u8cloud/api/arap/query", Description: "查询应收单"},
		}),
		BuiltIn: true,
		Enabled: true,
		Remark:  "用友 U8 cloud / YonBIP / NC Cloud 通用对接（财务 + 供应链）",
	}
}

func kingdeeTemplate() *model.IntegrationTemplate {
	return &model.IntegrationTemplate{
		Code:     "kingdee_erp_default",
		Platform: model.PlatformKingdee,
		Category: model.CategoryERP,
		Name:     "金蝶 K3Cloud / EAS 财务供应链对接模板",
		Version:  "1.0.0",
		APIBase:  "https://api.kingdee.com",
		AuthType: model.AuthTypeOAuth2,
		AuthConfig: `{
			"client_id": "",
			"client_secret": "",
			"acct_id": "",
			"org_id": ""
		}`,
		DocURL: "https://openapi.kingdee.com/",
		FieldMaps: mustJSON([]model.FieldMapping{
			{Source: srcCustomerUnifiedID, Target: "FCustId", Type: "string"},
			{Source: srcCustomerName, Target: "FName", Type: "string", Required: true},
			{Source: srcCustomerPhone, Target: "FPhone", Type: "string"},
			{Source: srcCustomerAddress, Target: "FAddress", Type: "string"},
			{Source: srcCustomerCompany, Target: "FCompanyName", Type: "string"},
			{Source: srcOrderID, Target: "FBillNo", Type: "string"},
			{Source: srcOrderPrice, Target: "FAmount", Type: "number", Transform: "yuan_to_fen"},
			{Source: srcOrderTime, Target: "FDate", Type: "date"},
		}),
		Endpoints: mustJSON([]model.EndpointConfig{
			{Name: "登录", Method: "POST", Path: "/Kingdee.BOS.WebApi.ServicesStub.AuthService.LoginByUserCredential.common.kdsvc", Description: "用户登录"},
			{Name: "客户保存", Method: "POST", Path: "/Kingdee.BOS.WebApi.ServicesStub.DynamicFormService.Save.common.kdsvc", Description: "保存客户档案"},
			{Name: "销售订单查询", Method: "POST", Path: "/Kingdee.BOS.WebApi.ServicesStub.DynamicFormService.View.common.kdsvc", Description: "查询销售订单"},
		}),
		BuiltIn: true,
		Enabled: true,
		Remark:  "金蝶云星空 K3Cloud / EAS / Cosmic 通用对接",
	}
}

func graspTemplate() *model.IntegrationTemplate {
	return &model.IntegrationTemplate{
		Code:     "grasp_erp_default",
		Platform: model.PlatformGrasp,
		Category: model.CategoryERP,
		Name:     "管家婆辉煌/财贸/工贸 ERP 对接模板",
		Version:  "1.0.0",
		APIBase:  "http://127.0.0.1:8088",
		AuthType: model.AuthTypeHMAC,
		AuthConfig: `{
			"server_no": "",
			"server_key": "",
			"sign_method": "MD5"
		}`,
		DocURL: "https://www.grasp.com.cn/help",
		FieldMaps: mustJSON([]model.FieldMapping{
			{Source: srcCustomerUnifiedID, Target: "custCode", Type: "string", Required: true},
			{Source: srcCustomerName, Target: "custName", Type: "string", Required: true},
			{Source: srcCustomerPhone, Target: "tel", Type: "string"},
			{Source: srcCustomerAddress, Target: "addr", Type: "string"},
			{Source: srcOrderID, Target: "billNo", Type: "string"},
			{Source: srcOrderPrice, Target: "totalMoney", Type: "number"},
			{Source: srcOrderTime, Target: "billDate", Type: "date"},
		}),
		Endpoints: mustJSON([]model.EndpointConfig{
			{Name: "客户档案保存", Method: "POST", Path: "/api/customer/save", Description: "保存客户档案"},
			{Name: "销售单保存", Method: "POST", Path: "/api/saleorder/save", Description: "保存销售单"},
			{Name: "库存查询", Method: "POST", Path: "/api/stock/query", Description: "查询库存"},
		}),
		BuiltIn: true,
		Enabled: true,
		Remark:  "管家婆辉煌/财贸/工贸系列通用对接（HMAC 签名）",
	}
}

func sapTemplate() *model.IntegrationTemplate {
	return &model.IntegrationTemplate{
		Code:     "sap_s4hana_default",
		Platform: model.PlatformSAP,
		Category: model.CategoryERP,
		Name:     "SAP S/4HANA / ECC OData 对接模板",
		Version:  "1.0.0",
		APIBase:  "https://myXXXXXX.sapbydesign.com/sap/byd/odata/v1",
		AuthType: model.AuthTypeOAuth2,
		AuthConfig: `{
			"client_id": "",
			"client_secret": "",
			"token_url": "",
			"scope": "API_BUSINESS_PARTNER_0001"
		}`,
		DocURL: "https://api.sap.com/package/ODPBusinessPartner",
		FieldMaps: mustJSON([]model.FieldMapping{
			{Source: srcCustomerUnifiedID, Target: "BusinessPartner", Type: "string", Required: true},
			{Source: srcCustomerName, Target: "BusinessPartnerName", Type: "string", Required: true},
			{Source: srcCustomerPhone, Target: "PhoneNumber", Type: "string"},
			{Source: srcCustomerEmail, Target: "EmailAddress", Type: "string"},
			{Source: srcCustomerAddress, Target: "AddressLine", Type: "string"},
			{Source: srcOrderID, Target: "SalesOrder", Type: "string"},
			{Source: srcOrderPrice, Target: "TotalNetAmount", Type: "number", Transform: "sap_currency"},
			{Source: srcOrderTime, Target: "SalesOrderDate", Type: "date"},
		}),
		Endpoints: mustJSON([]model.EndpointConfig{
			{Name: "客户主数据查询", Method: "GET", Path: "/BusinessPartner", Description: "查询业务伙伴主数据"},
			{Name: "客户主数据创建", Method: "POST", Path: "/BusinessPartner", Description: "创建业务伙伴"},
			{Name: "销售订单查询", Method: "GET", Path: "/A_SalesOrder", Description: "查询销售订单"},
			{Name: "销售订单创建", Method: "POST", Path: "/A_SalesOrder", Description: "创建销售订单"},
		}),
		BuiltIn: true,
		Enabled: true,
		Remark:  "SAP S/4HANA / ECC OData API 通用对接（业务伙伴 + 销售订单）",
	}
}

// All 返回所有预置模板（按插入顺序）
func All() []*model.IntegrationTemplate {
	return []*model.IntegrationTemplate{
		dingTalkTemplate(),
		weComTemplate(),
		feishuTemplate(),
		yonyouTemplate(),
		kingdeeTemplate(),
		graspTemplate(),
		sapTemplate(),
	}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
