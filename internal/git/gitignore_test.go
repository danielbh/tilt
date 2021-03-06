package git_test

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/windmilleng/tilt/internal/git"
	"github.com/windmilleng/tilt/internal/model"
	"github.com/windmilleng/tilt/internal/testutils/output"
	"github.com/windmilleng/tilt/internal/testutils/tempdir"
)

func TestGitIgnoreTester_Simple(t *testing.T) {
	tf := newTestFixture(t, ".*.swp")
	defer tf.TearDown()

	tf.UseGitIgnoreTester()

	tf.AssertResult(tf.JoinPath(0, "a", "b", ".foo.swp"), true, false)
}

func TestNewGitIgnoreTester_NoGitignore(t *testing.T) {
	tempDir := tempdir.NewTempDirFixture(t)
	defer tempDir.TearDown()

	g, err := git.NewGitIgnoreTesterFromContents(output.CtxForTest(), tempDir.Path(), "")
	if err != nil {
		t.Fatal(err)
	}

	// we were really just looking for a lack of error on initialization
	ret, err := g.Matches(tempDir.JoinPath("a", "b", ".foo.swp"), false)
	assert.Nil(t, err)
	assert.False(t, ret)

	ret, err = g.Matches(tempDir.JoinPath("foo.txt"), false)
	if assert.NoError(t, err) {
		assert.False(t, ret)
	}

}

func TestGitIgnoreTester_FileOutsideOfRepo(t *testing.T) {
	tf := newTestFixture(t, ".*.swp")
	defer tf.TearDown()

	tf.UseSingleRepoTester()
	tf.AssertResult(filepath.Join("/tmp", ".foo.swp"), false, false)
}

func TestGitIgnoreTester_GitDirMatches(t *testing.T) {
	tf := newTestFixture(t, ".*.swp")
	defer tf.TearDown()

	tf.UseSingleRepoTester()
	tf.AssertResult(tf.JoinPath(0, ".git", "foo", "bar"), true, false)
}

func TestGitIgnoreTester_DirectoryContents(t *testing.T) {
	t.Skip("https://app.clubhouse.io/windmill/story/1397/tilt-gitignore-doesn-t-ignore-directories")
	tf := newTestFixture(t, "foo/")
	defer tf.TearDown()

	tf.UseSingleRepoTester()

	tf.AssertResult("foo/bar", true, false)
}

func TestRepoIgnoreTester_MatchesRelativePath(t *testing.T) {
	tf := newTestFixture(t, "")
	defer tf.TearDown()

}

type testFixture struct {
	repoRoots []*tempdir.TempDirFixture
	tester    model.PathMatcher
	ctx       context.Context
	t         *testing.T
}

// initializes `tf.repoRoots` to be an array with one dir per gitignore
func newTestFixture(t *testing.T, gitignores ...string) *testFixture {
	tf := testFixture{}
	for _, gitignore := range gitignores {
		tempDir := tempdir.NewTempDirFixture(t)
		tempDir.WriteFile(".gitignore", gitignore)
		tf.repoRoots = append(tf.repoRoots, tempDir)
	}

	tf.ctx = context.Background()
	tf.t = t
	return &tf
}

func (tf *testFixture) UseGitIgnoreTester() {
	gitignorePath := tf.JoinPath(0, ".gitignore")
	contents, err := ioutil.ReadFile(gitignorePath)
	if err != nil {
		if !os.IsNotExist(err) {
			tf.t.Fatal(err)
		}
	}
	tester, err := git.NewGitIgnoreTesterFromContents(output.CtxForTest(), tf.repoRoots[0].Path(), string(contents))
	if err != nil {
		tf.t.Fatal(err)
	}

	tf.tester = tester
}

func (tf *testFixture) UseSingleRepoTester() {
	tf.UseSingleRepoTesterWithPath(tf.repoRoots[0].Path())
}

func (tf *testFixture) UseSingleRepoTesterWithPath(path string) {
	gitignoreContents, err := ioutil.ReadFile(filepath.Join(tf.repoRoots[0].JoinPath(".gitignore")))
	if err != nil {
		tf.t.Fatal(err)
	}
	tester, err := git.NewRepoIgnoreTester(tf.ctx, path, string(gitignoreContents))
	if err != nil {
		tf.t.Fatal(err)
	}
	tf.tester = tester
}

func (tf *testFixture) JoinPath(repoNum int, path ...string) string {
	return tf.repoRoots[repoNum].JoinPath(path...)
}

func (tf *testFixture) AssertResult(path string, expectedMatches bool, expectError bool) {
	isIgnored, err := tf.tester.Matches(path, false)
	if expectError {
		assert.Error(tf.t, err)
	} else {
		if assert.NoError(tf.t, err) {
			assert.Equal(tf.t, expectedMatches, isIgnored)
		}
	}
}

func (tf *testFixture) TearDown() {
	for _, tempDir := range tf.repoRoots {
		tempDir.TearDown()
	}
}
