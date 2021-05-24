/*
 * Tencent is pleased to support the open source community by making 蓝鲸 available.
 * Copyright (C) 2017-2018 THL A29 Limited, a Tencent company. All rights reserved.
 * Licensed under the MIT License (the "License"); you may not use this file except
 * in compliance with the License. You may obtain a copy of the License at
 * http://opensource.org/licenses/MIT
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions and
 * limitations under the License.
 */

package logics

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"configcenter/src/common"
	"configcenter/src/common/blog"
	"configcenter/src/common/condition"
	"configcenter/src/common/errors"
	lang "configcenter/src/common/language"
	"configcenter/src/common/mapstr"
	"configcenter/src/common/metadata"
	"configcenter/src/common/util"
	"configcenter/src/scene_server/host_server/logics"

	"github.com/gin-gonic/gin"
	"github.com/rentiansheng/xlsx"
)

// BuildExcelFromData product excel from data
func (lgc *Logics) BuildExcelFromData(ctx context.Context, objID string, fields map[string]Property, filter []string,
	data []mapstr.MapStr, xlsxFile *xlsx.File, header http.Header, modelBizID int64, usernameMap map[string]string,
	propertyList []string, objectUniqueIDMap map[string]int64) error {
	rid := util.GetHTTPCCRequestID(header)

	ccLang := lgc.Language.CreateDefaultCCLanguageIf(util.GetLanguage(header))
	ccErr := lgc.CCErr.CreateDefaultCCErrorIf(util.GetLanguage(header))
	sheet, err := xlsxFile.AddSheet("inst")
	if err != nil {
		blog.Errorf("setExcelRowDataByIndex add excel sheet error, err:%s, rid:%s", err.Error(), rid)
		return err

	}
	addSystemField(fields, common.BKInnerObjIDObject, ccLang)

	if 0 == len(filter) {
		filter = getFilterFields(objID)
	} else {
		filter = append(filter, getFilterFields(objID)...)
	}

	productExcelHeader(ctx, fields, filter, sheet, ccLang)
	// indexID := getFieldsIDIndexMap(fields)

	rowIndex := common.HostAddMethodExcelIndexOffset
	instIDArr := make([]int64, 64)

	for _, rowMap := range data {

		instIDKey := metadata.GetInstIDFieldByObjID(objID)
		instID, err := rowMap.Int64(instIDKey)
		if err != nil {
			blog.Errorf("setExcelRowDataByIndex inst:%+v, not inst id key:%s, objID:%s, rid:%s", rowMap, instIDKey, objID, rid)
			return ccErr.Errorf(common.CCErrCommInstFieldNotFound, "instIDKey", objID)
		}
		// 使用中英文用户名重新构造用户列表(用户列表实际为逗号分隔的string型)
		rowMap, err = replaceEnName(rid, rowMap, usernameMap, propertyList)
		if err != nil {
			blog.Errorf("rebuild user list field, rid: %s", rid)
			return err
		}

		setExcelRowDataByIndex(rowMap, sheet, rowIndex, fields)

		instIDArr = append(instIDArr, instID)
		rowIndex++

	}

	err = lgc.BuildAssociationExcelFromData(ctx, objID, instIDArr, xlsxFile, header, modelBizID, objectUniqueIDMap)
	if err != nil {
		return err
	}
	return nil
}

// BuildHostExcelFromData product excel from data
func (lgc *Logics) BuildHostExcelFromData(ctx context.Context, objID string, fields map[string]Property,
	filter []string, data []mapstr.MapStr, xlsxFile *xlsx.File, header http.Header, modelBizID int64,
	usernameMap map[string]string, propertyList []string, objectUniqueIDMap map[string]int64) error {
	rid := util.ExtractRequestIDFromContext(ctx)
	ccLang := lgc.Language.CreateDefaultCCLanguageIf(util.GetLanguage(header))
	ccErr := lgc.CCErr.CreateDefaultCCErrorIf(util.GetLanguage(header))

	sheet, err := xlsxFile.AddSheet("host")
	if err != nil {
		blog.Errorf("BuildHostExcelFromData add excel sheet error, err:%s, rid:%s", err.Error(), rid)
		return err
	}
	extFieldsTopoID := "cc_ext_field_topo"
	extFieldsBizID := "cc_ext_biz"
	extFields := map[string]string{
		extFieldsTopoID: ccLang.Language("web_ext_field_topo"),
		extFieldsBizID:  ccLang.Language("biz_property_bk_biz_name"),
	}
	fields = addExtFields(fields, extFields)
	addSystemField(fields, common.BKInnerObjIDHost, ccLang)

	productExcelHeader(ctx, fields, filter, sheet, ccLang)

	instIDArr := make([]int64, 0)
	// indexID := getFieldsIDIndexMap(fields)
	rowIndex := common.HostAddMethodExcelIndexOffset
	for _, hostData := range data {

		rowMap, err := mapstr.NewFromInterface(hostData[common.BKInnerObjIDHost])
		if err != nil {
			blog.ErrorJSON("BuildHostExcelFromData failed, hostData: %s, err: %s, rid: %s", hostData, err.Error(), rid)
			return ccErr.CCError(common.CCErrCommReplyDataFormatError)
		}

		if _, exist := fields[common.BKCloudIDField]; exist {
			cloudAreaArr, err := rowMap.MapStrArray(common.BKCloudIDField)
			if err != nil {
				blog.ErrorJSON("build host excel failed, cloud area not array, host: %s, err: %s, rid: %s", hostData, err, rid)
				return ccErr.CCError(common.CCErrCommReplyDataFormatError)
			}

			if len(cloudAreaArr) != 1 {
				blog.ErrorJSON("build host excel failed, host has many cloud areas, host: %s, err: %s, rid: %s", hostData, err, rid)
				return ccErr.CCError(common.CCErrCommReplyDataFormatError)
			}

			cloudArea := fmt.Sprintf("%v[%v]", cloudAreaArr[0][common.BKInstNameField], cloudAreaArr[0][common.BKInstIDField])
			rowMap.Set(common.BKCloudIDField, cloudArea)
		}

		// set extended fields
		moduleMap, ok := hostData[common.BKInnerObjIDModule].([]interface{})
		if ok {
			topos := util.GetStrValsFromArrMapInterfaceByKey(moduleMap, "TopModuleName")
			if len(topos) > 0 {
				idx := strings.Index(topos[0], logics.SplitFlag)
				if idx > 0 {
					rowMap[extFieldsBizID] = topos[0][:idx]
				}

				toposNobiz := make([]string, 0)
				for _, topo := range topos {
					idx := strings.Index(topo, logics.SplitFlag)
					if idx > 0 && len(topo) >= idx+len(logics.SplitFlag) {
						toposNobiz = append(toposNobiz, topo[idx+len(logics.SplitFlag):])
					}
				}
				rowMap[extFieldsTopoID] = strings.Join(toposNobiz, "\n")
			}
		}

		instIDKey := metadata.GetInstIDFieldByObjID(objID)
		instID, err := rowMap.Int64(instIDKey)
		if err != nil {
			blog.Errorf("setExcelRowDataByIndex inst:%+v, not inst id key:%s, objID:%s, rid:%s", rowMap, instIDKey, objID, rid)
			return ccErr.Errorf(common.CCErrCommInstFieldNotFound, instIDKey, objID)
		}

		// 使用中英文用户名重新构造用户列表(用户列表实际为逗号分隔的string型)
		rowMap, err = replaceEnName(rid, rowMap, usernameMap, propertyList)
		if err != nil {
			blog.Errorf("rebuild user list field, rid: %s", rid)
			return err
		}

		setExcelRowDataByIndex(rowMap, sheet, rowIndex, fields)
		instIDArr = append(instIDArr, instID)
		rowIndex++
	}

	err = lgc.BuildAssociationExcelFromData(ctx, objID, instIDArr, xlsxFile, header, modelBizID, objectUniqueIDMap)
	if err != nil {
		return err
	}

	return nil
}

// 获取模型关联关系
func (lgc *Logics) getObjectAssociation(ctx context.Context, header http.Header, objID string, modelBizID int64) (
	[]*metadata.Association, errors.CCErrorCoder) {
	rid := util.ExtractRequestIDFromContext(ctx)
	defErr := lgc.CCErr.CreateDefaultCCErrorIf(util.GetLanguage(header))

	cond := &metadata.SearchAssociationObjectRequest{
		Condition: map[string]interface{}{
			condition.BKDBOR: []mapstr.MapStr{
				{
					common.BKObjIDField: objID,
				},
				{
					common.BKAsstObjIDField: objID,
				},
			},
		},
	}
	//确定关联标识的列表，定义excel选项下拉栏。此处需要查cc_ObjAsst表。
	resp, err := lgc.CoreAPI.ApiServer().SearchObjectAssociation(ctx, header, cond)
	if err != nil {
		blog.ErrorJSON("get object association list failed, err: %v, rid: %s", err, rid)
		return nil, defErr.CCError(common.CCErrCommHTTPDoRequestFailed)
	}
	if err := resp.CCError(); err != nil {
		blog.ErrorJSON("get object association list failed, err: %v, rid: %s", resp.ErrMsg, rid)
		return nil, err
	}

	return resp.Data, nil
}

// BuildAssociationExcelFromData
// @Params: objectUniqueIDMap 用来限定导出关联模型ID 和 导出数据使用使用的唯一索引
func (lgc *Logics) BuildAssociationExcelFromData(ctx context.Context, objID string, instIDArr []int64,
	xlsxFile *xlsx.File, header http.Header, modelBizID int64, objectUniqueIDMap map[string]int64) error {

	rid := util.ExtractRequestIDFromContext(ctx)
	defLang := lgc.Language.CreateDefaultCCLanguageIf(util.GetLanguage(header))

	sheet, err := xlsxFile.AddSheet("association")
	if err != nil {
		blog.Errorf("setExcelRowDataByIndex add excel  association sheet error. err:%s, rid:%s", err.Error(), rid)
		return err
	}

	asstList, err := lgc.getObjectAssociation(ctx, header, objID, modelBizID)
	if err != nil {
		return err
	}
	// 未设置, 不导出关联关系数据
	if len(objectUniqueIDMap) == 0 {
		productExcelAssociationHeader(ctx, sheet, defLang, 0, asstList)
		return nil
	}

	instAsst, err := lgc.fetchAssociationData(ctx, header, objID, instIDArr, modelBizID)
	if err != nil {
		return err
	}
	asstData, err := lgc.getAssociationData(ctx, header, objID, instAsst, modelBizID, objectUniqueIDMap)
	if err != nil {
		return err
	}

	productExcelAssociationHeader(ctx, sheet, defLang, len(instAsst), asstList)

	rowIndex := common.HostAddMethodExcelAssociationIndexOffset

	for _, inst := range instAsst {
		sheet.Cell(rowIndex, 1).SetString(inst.ObjectAsstID)
		sheet.Cell(rowIndex, 2).SetString("")
		srcInst, ok := asstData[inst.ObjectID][inst.InstID]
		if !ok {
			blog.Warnf("BuildAssociationExcelFromData association inst:%+v, not inst id :%d, objID:%s, rid:%s", inst, inst.InstID, objID, rid)
			// return lgc.CCErr.CreateDefaultCCErrorIf(util.GetLanguage(header)).Errorf(common.CCErrCommInstDataNil, fmt.Sprintf("%s %d", objID, inst.InstID))
			continue
		}
		dstInst, ok := asstData[inst.AsstObjectID][inst.AsstInstID]
		if !ok {
			blog.Warnf("BuildAssociationExcelFromData association inst:%+v, not inst id :%d, objID:%s, rid:%s", inst, inst.InstID, inst.AsstObjectID, rid)
			continue
		}

		// TODO: 注意源和目标顺序， 手续需要将当前对象放到最开始的位置
		sheet.Cell(rowIndex, 3).SetString(buildEexcelPrimaryKey(srcInst))
		sheet.Cell(rowIndex, 4).SetString(buildEexcelPrimaryKey(dstInst))
		style := sheet.Cell(rowIndex, 3).GetStyle()
		style.Alignment.WrapText = true
		style = sheet.Cell(rowIndex, 4).GetStyle()
		style.Alignment.WrapText = true
		rowIndex++
	}

	return nil

}

func buildEexcelPrimaryKey(propertyArr []PropertyPrimaryVal) string {
	var contentArr []string
	for _, property := range propertyArr {
		contentArr = append(contentArr, buildExcelPrimaryStr(property))
	}
	return strings.Join(contentArr, common.ExcelAsstPrimaryKeySplitChar)
}

func buildExcelPrimaryStr(property PropertyPrimaryVal) string {
	return property.Name + common.ExcelAsstPrimaryKeyJoinChar + property.StrVal
}

// BuildExcelTemplate  return httpcode, error
func (lgc *Logics) BuildExcelTemplate(ctx context.Context, objID, filename string, header http.Header, defLang lang.DefaultCCLanguageIf, modelBizID int64) error {
	rid := util.GetHTTPCCRequestID(header)
	filterFields := getFilterFields(objID)
	// host excel template doesn't need export field bk_cloud_id
	if objID == common.BKInnerObjIDHost {
		filterFields = append(filterFields, common.BKCloudIDField)
	}
	fields, err := lgc.GetObjFieldIDs(objID, filterFields, nil, header, modelBizID)
	if err != nil {
		blog.Errorf("get %s fields error:%s, rid: %s", objID, err.Error(), rid)
		return err
	}

	var file *xlsx.File
	file = xlsx.NewFile()
	sheet, err := file.AddSheet(objID)
	if err != nil {
		blog.Errorf("get %s fields error: %v, rid: %s", objID, err, rid)
		return err
	}
	blog.V(5).Infof("BuildExcelTemplate fields count:%d, rid: %s", fields, rid)
	productExcelHeader(ctx, fields, filterFields, sheet, defLang)
	ProductExcelCommentSheet(ctx, file, defLang)

	if err = file.Save(filename); nil != err {
		blog.Errorf("save file failed, filename: %s, err: %+v, rid: %s", filename, err, rid)
		return err
	}

	return nil
}

func AddDownExcelHttpHeader(c *gin.Context, name string) {
	if strings.HasSuffix(name, ".xls") {
		c.Header("Content-Type", "application/vnd.ms-excel")
	} else {
		c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	}
	c.Header("Accept-Ranges", "bytes")
	c.Header("Content-Disposition", "attachment; filename="+name) // 文件名
	c.Header("Cache-Control", "must-revalidate, post-check=0, pre-check=0")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
}

// GetExcelData excel数据，一个kv结构，key行数（excel中的行数），value内容
func GetExcelData(ctx context.Context, sheet *xlsx.Sheet, fields map[string]Property, defFields common.KvMap, isCheckHeader bool, firstRow int, defLang lang.DefaultCCLanguageIf) (map[int]map[string]interface{}, []string, error) {

	var err error
	nameIndexMap, err := checkExcelHeader(ctx, sheet, fields, isCheckHeader, defLang)
	if nil != err {
		return nil, nil, err
	}
	hosts := make(map[int]map[string]interface{})
	index := headerRow
	if 0 != firstRow {
		index = firstRow
	}
	errMsg := make([]string, 0)
	rowCnt := len(sheet.Rows)
	for ; index < rowCnt; index++ {
		row := sheet.Rows[index]
		host, getErr := getDataFromByExcelRow(ctx, row, index, fields, defFields, nameIndexMap, defLang)
		if 0 != len(getErr) {
			errMsg = append(errMsg, getErr...)
			continue
		}
		if 0 != len(host) {
			hosts[index+1] = host
		}
	}
	if 0 != len(errMsg) {
		return nil, errMsg, nil
	}

	return hosts, nil, nil

}

// GetExcelData excel数据，一个kv结构，key行数（excel中的行数），value内容
func GetRawExcelData(ctx context.Context, sheet *xlsx.Sheet, defFields common.KvMap, firstRow int, defLang lang.DefaultCCLanguageIf) (map[int]map[string]interface{}, []string, error) {

	var err error
	nameIndexMap, err := checkExcelHeader(ctx, sheet, nil, false, defLang)
	if nil != err {
		return nil, nil, err
	}
	hosts := make(map[int]map[string]interface{})
	index := headerRow
	if 0 != firstRow {
		index = firstRow
	}
	errMsg := make([]string, 0)
	rowCnt := len(sheet.Rows)
	for ; index < rowCnt; index++ {
		row := sheet.Rows[index]
		host, getErr := getDataFromByExcelRow(ctx, row, index, nil, defFields, nameIndexMap, defLang)
		if nil != getErr {
			errMsg = append(errMsg, getErr...)
			continue
		}
		if 0 == len(host) {
			hosts[index+1] = nil
		} else {
			hosts[index+1] = host
		}
	}
	if 0 != len(errMsg) {
		return nil, errMsg, nil
	}

	return hosts, nil, nil

}

func GetAssociationExcelData(sheet *xlsx.Sheet, firstRow int) map[int]metadata.ExcelAssociation {

	rowCnt := len(sheet.Rows)
	index := firstRow

	asstInfoArr := make(map[int]metadata.ExcelAssociation, 0)
	for ; index < rowCnt; index++ {
		row := sheet.Rows[index]
		op := row.Cells[associationOPColIndex].String()
		if op == "" {
			continue
		}

		asstObjID := row.Cells[associationAsstObjIDIndex].String()
		srcInst := row.Cells[associationSrcInstIndex].String()
		dstInst := row.Cells[associationDstInstIndex].String()
		asstInfoArr[index] = metadata.ExcelAssociation{
			ObjectAsstID: asstObjID,
			Operate:      getAssociationExcelOperateFlag(op),
			SrcPrimary:   srcInst,
			DstPrimary:   dstInst,
		}
	}

	return asstInfoArr
}

func StatisticsAssociation(sheet *xlsx.Sheet, firstRow int) ([]string, map[string]metadata.ObjectAsstIDStatisticsInfo) {

	rowCnt := len(sheet.Rows)
	index := firstRow
	asstInfoMap := make(map[string]metadata.ObjectAsstIDStatisticsInfo, 0)
	//bk_obj_asst_id
	asstNameArr := make([]string, 0)
	for ; index < rowCnt; index++ {
		var asstInfo metadata.ObjectAsstIDStatisticsInfo
		var ok bool

		row := sheet.Rows[index]

		// bk_obj_asst_id 的值
		asstObjID := row.Cells[associationAsstObjIDIndex].String()
		asstInfo, ok = asstInfoMap[asstObjID]
		if !ok {
			// 第一次出现这个关联关系的唯一标识
			asstNameArr = append(asstNameArr, asstObjID)
			asstInfo = metadata.ObjectAsstIDStatisticsInfo{}
		}
		asstInfo.Total += 1
		op := row.Cells[associationOPColIndex].String()
		if op == "" {
			continue
		} else {
			operate := getAssociationExcelOperateFlag(op)
			switch operate {
			case metadata.ExcelAssociationOperateDelete:
				asstInfo.Delete += 1
			case metadata.ExcelAssociationOperateAdd:
				asstInfo.Create += 1
			}
		}
		asstInfoMap[asstObjID] = asstInfo
	}

	return asstNameArr, asstInfoMap
}

// GetFilterFields 不需要展示字段
func GetFilterFields(objID string) []string {
	return getFilterFields(objID)
}

// GetCustomFields 用户展示字段export时优先排序
func GetCustomFields(filterFields []string, customFields []string) []string {
	return getCustomFields(filterFields, customFields)
}

func getAssociationExcelOperateFlag(op string) metadata.ExcelAssociationOperate {
	opFlag := metadata.ExcelAssociationOperateError
	switch op {
	case associationOPAdd:
		opFlag = metadata.ExcelAssociationOperateAdd
	case associationOPDelete:
		opFlag = metadata.ExcelAssociationOperateDelete
	}

	return opFlag
}
