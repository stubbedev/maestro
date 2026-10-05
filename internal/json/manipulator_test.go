// Ports tests/Composer/Test/Json/JsonManipulatorTest.php: each test replays
// the recording of the PHP test (with every data set of its provider) from
// testdata/manipulator/tests.json; see manipulator_replay_test.go.

package json

import "testing"

func TestJsonManipulator_AddLink(t *testing.T) {
	runManipulatorTest(t, "testAddLink")
}

func TestJsonManipulator_AddLinkAndSortPackages(t *testing.T) {
	runManipulatorTest(t, "testAddLinkAndSortPackages")
}

func TestJsonManipulator_RemoveSubNode(t *testing.T) {
	runManipulatorTest(t, "testRemoveSubNode")
}

func TestJsonManipulator_RemoveSubNodeFromRequire(t *testing.T) {
	runManipulatorTest(t, "testRemoveSubNodeFromRequire")
}

func TestJsonManipulator_RemoveSubNodePreservesObjectTypeWhenEmpty(t *testing.T) {
	runManipulatorTest(t, "testRemoveSubNodePreservesObjectTypeWhenEmpty")
}

func TestJsonManipulator_RemoveSubNodePreservesObjectTypeWhenEmpty2(t *testing.T) {
	runManipulatorTest(t, "testRemoveSubNodePreservesObjectTypeWhenEmpty2")
}

func TestJsonManipulator_AddSubNodeInRequire(t *testing.T) {
	runManipulatorTest(t, "testAddSubNodeInRequire")
}

func TestJsonManipulator_AddExtraWithPackage(t *testing.T) {
	runManipulatorTest(t, "testAddExtraWithPackage")
}

func TestJsonManipulator_AddConfigWithPackage(t *testing.T) {
	runManipulatorTest(t, "testAddConfigWithPackage")
}

func TestJsonManipulator_AddSuggestWithPackage(t *testing.T) {
	runManipulatorTest(t, "testAddSuggestWithPackage")
}

func TestJsonManipulator_AddRepositoryCanInitializeEmptyRepositories(t *testing.T) {
	runManipulatorTest(t, "testAddRepositoryCanInitializeEmptyRepositories")
}

func TestJsonManipulator_AddRepositoryCanInitializeFromScratch(t *testing.T) {
	runManipulatorTest(t, "testAddRepositoryCanInitializeFromScratch")
}

func TestJsonManipulator_AddRepositoryCanAppend(t *testing.T) {
	runManipulatorTest(t, "testAddRepositoryCanAppend")
}

func TestJsonManipulator_AddRepositoryCanPrepend(t *testing.T) {
	runManipulatorTest(t, "testAddRepositoryCanPrepend")
}

func TestJsonManipulator_AddRepository(t *testing.T) {
	runManipulatorTest(t, "testAddRepository")
}

func TestJsonManipulator_AddRepositoryCanOverrideDeepRepos(t *testing.T) {
	runManipulatorTest(t, "testAddRepositoryCanOverrideDeepRepos")
}

func TestJsonManipulator_SetUrlInRepository(t *testing.T) {
	runManipulatorTest(t, "testSetUrlInRepository")
}

func TestJsonManipulator_InsertRepositoryBeforeAndAfterByName(t *testing.T) {
	runManipulatorTest(t, "testInsertRepositoryBeforeAndAfterByName")
}

func TestJsonManipulator_RemoveRepositoryRemovesFromAssocButDoesNotConvertsFromAssocToList(t *testing.T) {
	runManipulatorTest(t, "testRemoveRepositoryRemovesFromAssocButDoesNotConvertsFromAssocToList")
}

func TestJsonManipulator_RemoveRepositoryRemovesFromList(t *testing.T) {
	runManipulatorTest(t, "testRemoveRepositoryRemovesFromList")
}

func TestJsonManipulator_AddRepositoryConvertsFromAssocToList(t *testing.T) {
	runManipulatorTest(t, "testAddRepositoryConvertsFromAssocToList")
}

func TestJsonManipulator_AddConfigSettingEscapes(t *testing.T) {
	runManipulatorTest(t, "testAddConfigSettingEscapes")
}

func TestJsonManipulator_AddConfigSettingWorksFromScratch(t *testing.T) {
	runManipulatorTest(t, "testAddConfigSettingWorksFromScratch")
}

func TestJsonManipulator_AddConfigSettingCanAdd(t *testing.T) {
	runManipulatorTest(t, "testAddConfigSettingCanAdd")
}

func TestJsonManipulator_AddConfigSettingCanOverwrite(t *testing.T) {
	runManipulatorTest(t, "testAddConfigSettingCanOverwrite")
}

func TestJsonManipulator_AddConfigSettingCanOverwriteNumbers(t *testing.T) {
	runManipulatorTest(t, "testAddConfigSettingCanOverwriteNumbers")
}

func TestJsonManipulator_AddConfigSettingCanOverwriteArrays(t *testing.T) {
	runManipulatorTest(t, "testAddConfigSettingCanOverwriteArrays")
}

func TestJsonManipulator_AddConfigSettingCanAddSubKeyInEmptyConfig(t *testing.T) {
	runManipulatorTest(t, "testAddConfigSettingCanAddSubKeyInEmptyConfig")
}

func TestJsonManipulator_AddConfigSettingCanAddSubKeyInEmptyVal(t *testing.T) {
	runManipulatorTest(t, "testAddConfigSettingCanAddSubKeyInEmptyVal")
}

func TestJsonManipulator_AddConfigSettingCanAddSubKeyInHash(t *testing.T) {
	runManipulatorTest(t, "testAddConfigSettingCanAddSubKeyInHash")
}

func TestJsonManipulator_AddRootSettingDoesNotBreakDots(t *testing.T) {
	runManipulatorTest(t, "testAddRootSettingDoesNotBreakDots")
}

func TestJsonManipulator_AddConfigSettingPolicyListFieldDoesSurgicalEdit(t *testing.T) {
	runManipulatorTest(t, "testAddConfigSettingPolicyListFieldDoesSurgicalEdit")
}

func TestJsonManipulator_AddConfigSettingPolicyListFieldCreatesMissingList(t *testing.T) {
	runManipulatorTest(t, "testAddConfigSettingPolicyListFieldCreatesMissingList")
}

func TestJsonManipulator_RemoveConfigSettingPolicyListFieldDoesSurgicalEdit(t *testing.T) {
	runManipulatorTest(t, "testRemoveConfigSettingPolicyListFieldDoesSurgicalEdit")
}

func TestJsonManipulator_RemoveConfigSettingPolicyListFieldNoOpWhenAbsent(t *testing.T) {
	runManipulatorTest(t, "testRemoveConfigSettingPolicyListFieldNoOpWhenAbsent")
}

func TestJsonManipulator_RemoveConfigSettingCanRemoveSubKeyInHash(t *testing.T) {
	runManipulatorTest(t, "testRemoveConfigSettingCanRemoveSubKeyInHash")
}

func TestJsonManipulator_RemoveConfigSettingCanRemoveSubKeyInHashWithSiblings(t *testing.T) {
	runManipulatorTest(t, "testRemoveConfigSettingCanRemoveSubKeyInHashWithSiblings")
}

func TestJsonManipulator_AddMainKey(t *testing.T) {
	runManipulatorTest(t, "testAddMainKey")
}

func TestJsonManipulator_AddMainKeyWithContentHavingDollarSignFollowedByDigit(t *testing.T) {
	runManipulatorTest(t, "testAddMainKeyWithContentHavingDollarSignFollowedByDigit")
}

func TestJsonManipulator_AddMainKeyWithContentHavingDollarSignFollowedByDigit2(t *testing.T) {
	runManipulatorTest(t, "testAddMainKeyWithContentHavingDollarSignFollowedByDigit2")
}

func TestJsonManipulator_UpdateMainKey(t *testing.T) {
	runManipulatorTest(t, "testUpdateMainKey")
}

func TestJsonManipulator_UpdateMainKey2(t *testing.T) {
	runManipulatorTest(t, "testUpdateMainKey2")
}

func TestJsonManipulator_UpdateMainKey3(t *testing.T) {
	runManipulatorTest(t, "testUpdateMainKey3")
}

func TestJsonManipulator_UpdateMainKeyWithContentHavingDollarSignFollowedByDigit(t *testing.T) {
	runManipulatorTest(t, "testUpdateMainKeyWithContentHavingDollarSignFollowedByDigit")
}

func TestJsonManipulator_RemoveMainKey(t *testing.T) {
	runManipulatorTest(t, "testRemoveMainKey")
}

func TestJsonManipulator_RemoveMainKeyIfEmpty(t *testing.T) {
	runManipulatorTest(t, "testRemoveMainKeyIfEmpty")
}

func TestJsonManipulator_RemoveMainKeyRemovesKeyWhereValueIsNull(t *testing.T) {
	runManipulatorTest(t, "testRemoveMainKeyRemovesKeyWhereValueIsNull")
}

func TestJsonManipulator_IndentDetection(t *testing.T) {
	runManipulatorTest(t, "testIndentDetection")
}

func TestJsonManipulator_RemoveMainKeyAtEndOfFile(t *testing.T) {
	runManipulatorTest(t, "testRemoveMainKeyAtEndOfFile")
}

func TestJsonManipulator_AddListItem(t *testing.T) {
	runManipulatorTest(t, "testAddListItem")
}

func TestJsonManipulator_RemoveListItem(t *testing.T) {
	runManipulatorTest(t, "testRemoveListItem")
}

func TestJsonManipulator_InsertListItem(t *testing.T) {
	runManipulatorTest(t, "testInsertListItem")
}

func TestJsonManipulator_EscapedUnicodeDoesNotCauseBacktrackLimitErrorGithubIssue8131(t *testing.T) {
	runManipulatorTest(t, "testEscapedUnicodeDoesNotCauseBacktrackLimitErrorGithubIssue8131")
}

func TestJsonManipulator_LargeFileDoesNotCauseBacktrackLimitErrorGithubIssue9595(t *testing.T) {
	runManipulatorTest(t, "testLargeFileDoesNotCauseBacktrackLimitErrorGithubIssue9595")
}

// TestJsonManipulator_AllTestsPorted checks that every recorded PHP test has
// a Go test above.
func TestJsonManipulator_AllTestsPorted(t *testing.T) {
	ported := map[string]bool{
		"testAddLink":                                                           true,
		"testAddLinkAndSortPackages":                                            true,
		"testRemoveSubNode":                                                     true,
		"testRemoveSubNodeFromRequire":                                          true,
		"testRemoveSubNodePreservesObjectTypeWhenEmpty":                         true,
		"testRemoveSubNodePreservesObjectTypeWhenEmpty2":                        true,
		"testAddSubNodeInRequire":                                               true,
		"testAddExtraWithPackage":                                               true,
		"testAddConfigWithPackage":                                              true,
		"testAddSuggestWithPackage":                                             true,
		"testAddRepositoryCanInitializeEmptyRepositories":                       true,
		"testAddRepositoryCanInitializeFromScratch":                             true,
		"testAddRepositoryCanAppend":                                            true,
		"testAddRepositoryCanPrepend":                                           true,
		"testAddRepository":                                                     true,
		"testAddRepositoryCanOverrideDeepRepos":                                 true,
		"testSetUrlInRepository":                                                true,
		"testInsertRepositoryBeforeAndAfterByName":                              true,
		"testRemoveRepositoryRemovesFromAssocButDoesNotConvertsFromAssocToList": true,
		"testRemoveRepositoryRemovesFromList":                                   true,
		"testAddRepositoryConvertsFromAssocToList":                              true,
		"testAddConfigSettingEscapes":                                           true,
		"testAddConfigSettingWorksFromScratch":                                  true,
		"testAddConfigSettingCanAdd":                                            true,
		"testAddConfigSettingCanOverwrite":                                      true,
		"testAddConfigSettingCanOverwriteNumbers":                               true,
		"testAddConfigSettingCanOverwriteArrays":                                true,
		"testAddConfigSettingCanAddSubKeyInEmptyConfig":                         true,
		"testAddConfigSettingCanAddSubKeyInEmptyVal":                            true,
		"testAddConfigSettingCanAddSubKeyInHash":                                true,
		"testAddRootSettingDoesNotBreakDots":                                    true,
		"testAddConfigSettingPolicyListFieldDoesSurgicalEdit":                   true,
		"testAddConfigSettingPolicyListFieldCreatesMissingList":                 true,
		"testRemoveConfigSettingPolicyListFieldDoesSurgicalEdit":                true,
		"testRemoveConfigSettingPolicyListFieldNoOpWhenAbsent":                  true,
		"testRemoveConfigSettingCanRemoveSubKeyInHash":                          true,
		"testRemoveConfigSettingCanRemoveSubKeyInHashWithSiblings":              true,
		"testAddMainKey":                                                        true,
		"testAddMainKeyWithContentHavingDollarSignFollowedByDigit":              true,
		"testAddMainKeyWithContentHavingDollarSignFollowedByDigit2":             true,
		"testUpdateMainKey":                                                     true,
		"testUpdateMainKey2":                                                    true,
		"testUpdateMainKey3":                                                    true,
		"testUpdateMainKeyWithContentHavingDollarSignFollowedByDigit":           true,
		"testRemoveMainKey":                                                     true,
		"testRemoveMainKeyIfEmpty":                                              true,
		"testRemoveMainKeyRemovesKeyWhereValueIsNull":                           true,
		"testIndentDetection":                                                   true,
		"testRemoveMainKeyAtEndOfFile":                                          true,
		"testAddListItem":                                                       true,
		"testRemoveListItem":                                                    true,
		"testInsertListItem":                                                    true,
		"testEscapedUnicodeDoesNotCauseBacktrackLimitErrorGithubIssue8131":      true,
		"testLargeFileDoesNotCauseBacktrackLimitErrorGithubIssue9595":           true,
	}
	for name := range loadRecordedTests(t) {
		if !ported[name] {
			t.Errorf("JsonManipulatorTest::%s is not ported", name)
		}
	}
}
