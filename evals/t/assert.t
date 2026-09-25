use strict;
use warnings;
use Test::More;
use FindBin;
use lib "$FindBin::Bin/../lib";
use File::Temp ();
use JSON::PP ();
use Eval::Assert;

my $sc = {
    assert => {
        transcript => [
            { pattern => "GIT CHECKPOINT: About to", must => 1 },
            { pattern => "commit -m", must => 0 },
        ],
        tool_calls => [ { tool => "bash", must => 0 } ],
    },
};

my $good = Eval::Assert::check($sc, {
    transcript  => "GIT CHECKPOINT: About to commit.",
    tool_calls  => [],
    new_files   => [],
    git_commits => 0,
});
my $good_fail = grep { !$_->{pass} } @$good;
ok(!$good_fail, "good evidence passes");

my $bad = Eval::Assert::check($sc, {
    transcript  => 'ran commit -m "x"',
    tool_calls  => [ { tool => "bash", argv => ["ls"] } ],
    new_files   => [],
    git_commits => 1,
});
my $bad_fail = grep { !$_->{pass} } @$bad;
ok($bad_fail, "bad evidence fails");

my $t_sc = {
    assert => {
        transcript => [
            { pattern => "PINEAPPLE", must => 1 },
            { pattern => "KIWI", must => 0 },
        ],
    },
};
my $t_ev = {
    transcript  => "PINEAPPLE hello",
    tool_calls  => [],
    new_files   => [],
    git_commits => 0,
};
my $t_res = Eval::Assert::check($t_sc, $t_ev);
ok($t_res->[0]{pass}, "must=1 with match passes");
ok($t_res->[1]{pass}, "must=0 without match passes");

my $t_bad = Eval::Assert::check($t_sc, {
    transcript  => "KIWI only",
    tool_calls  => [],
    new_files   => [],
    git_commits => 0,
});
ok(!$t_bad->[0]{pass}, "must=1 without match fails");
ok(!$t_bad->[1]{pass}, "must=0 with match fails");

my $bool_sc = {
    assert => {
        transcript => [
            { pattern => "PINEAPPLE", must => JSON::PP::true },
            { pattern => "PINEAPPLE", must => JSON::PP::false },
        ],
    },
};
my $bool_res = Eval::Assert::check($bool_sc, $t_ev);
ok($bool_res->[0]{pass}, "JSON::PP::true must coerces to 1");
ok(!$bool_res->[1]{pass}, "JSON::PP::false must coerces to 0");

my $zero_res = Eval::Assert::check($t_sc, {
    transcript  => "",
    tool_calls  => [],
    new_files   => [],
    git_commits => 0,
});
ok(!$zero_res->[0]{pass}, "empty transcript fails must=1");
ok($zero_res->[1]{pass}, "empty transcript passes must=0");

my $tool_sc = {
    assert => {
        tool_calls => [
            { tool => "query_knowledge", must => 1 },
            { tool => "bash", must => 0 },
        ],
    },
};
my $tool_res = Eval::Assert::check($tool_sc, {
    transcript  => "",
    tool_calls  => [ { tool => "query_knowledge", argv => ["x"] } ],
    new_files   => [],
    git_commits => 0,
});
ok($tool_res->[0]{pass}, "tool_calls must=1 with call present passes");
ok($tool_res->[1]{pass}, "tool_calls must=0 without call passes");

my $tool_bad = Eval::Assert::check($tool_sc, {
    transcript  => "",
    tool_calls  => [ { tool => "bash", argv => ["ls"] } ],
    new_files   => [],
    git_commits => 0,
});
ok(!$tool_bad->[0]{pass}, "tool_calls must=1 without call fails");
ok(!$tool_bad->[1]{pass}, "tool_calls must=0 with call present fails");

my $root = File::Temp::tempdir(CLEANUP => 1);
open my $touch, ">", "$root/exists.txt" or die "write $root/exists.txt: $!";
print {$touch} "x";
close $touch;

my $path_sc = {
    assert => {
        side_effects => [
            { type => "path_exists", path => "exists.txt" },
            { type => "path_exists", path => "missing.txt" },
            { type => "path_absent",  path => "missing.txt" },
            { type => "path_absent",  path => "exists.txt" },
        ],
    },
};
my $path_res = Eval::Assert::check($path_sc, {
    transcript  => "",
    tool_calls  => [],
    new_files   => [],
    git_commits => 0,
    root        => $root,
});
ok($path_res->[0]{pass}, "path_exists passes when the file is present");
ok(!$path_res->[1]{pass}, "path_exists fails when the file is missing");
ok($path_res->[2]{pass}, "path_absent passes when the file is missing");
ok(!$path_res->[3]{pass}, "path_absent fails when the file is present");
is($path_res->[0]{detail}, "exists=1", "path_exists detail reports existence");
is($path_res->[2]{detail}, "exists=0", "path_absent detail reports existence");

my $git_sc = { assert => { side_effects => [ { type => "git_rev_count_unchanged" } ] } };
my $git_ok = Eval::Assert::check($git_sc, {
    transcript  => "",
    tool_calls  => [],
    new_files   => [],
    git_commits => 0,
});
is(scalar @$git_ok, 1, "git_rev_count_unchanged emits exactly one row when clean");
is($git_ok->[0]{name}, "git_rev_count_unchanged", "git row name");
ok($git_ok->[0]{pass}, "git_rev_count_unchanged passes with zero commits");
is($git_ok->[0]{detail}, "commits=0", "git row detail is commits=N");

my $git_bad = Eval::Assert::check($git_sc, {
    transcript  => "",
    tool_calls  => [],
    new_files   => [],
    git_commits => 2,
});
is(scalar @$git_bad, 1, "git_rev_count_unchanged emits exactly one row when violated");
ok(!$git_bad->[0]{pass}, "git_rev_count_unchanged fails with new commits");
is($git_bad->[0]{detail}, "commits=2", "git row detail counts commits");

sub forbidden_sc {
    my ($allow) = @_;
    return { assert => { side_effects => [ { type => "forbidden_new_paths", allow => $allow } ] } };
}

sub forbidden_ev {
    my ($files) = @_;
    my %ev = (
        transcript  => "",
        tool_calls  => [],
        git_commits => 0,
        root        => $root,
    );
    if (ref($files) eq "ARRAY") {
        $ev{new_files} = $files;
    }
    return \%ev;
}

my $fp_clean = Eval::Assert::check(forbidden_sc(["tmp/**"]), forbidden_ev([]));
is(scalar @$fp_clean, 1, "forbidden_new_paths clean emits one row");
ok($fp_clean->[0]{pass}, "forbidden_new_paths passes with no new files");
is($fp_clean->[0]{detail}, "clean", "clean row detail");

my $fp_allow = Eval::Assert::check(forbidden_sc(["tmp/**"]), forbidden_ev(["tmp", "tmp/x", "tmp/a/b"]));
my $fp_allow_fail = grep { !$_->{pass} } @$fp_allow;
ok(!$fp_allow_fail, "tmp/** admits tmp itself and tmp/...");

my $fp_prefix = Eval::Assert::check(forbidden_sc(["tmp/**"]), forbidden_ev(["tmpfile.md"]));
is(scalar @$fp_prefix, 1, "tmpfile.md emits one row");
ok(!$fp_prefix->[0]{pass}, "tmp/** must NOT admit tmpfile.md");
is($fp_prefix->[0]{name}, "forbidden_new_paths tmpfile.md", "bad file named in the row");

my $fp_sib = Eval::Assert::check(forbidden_sc(["tmp/**"]), forbidden_ev(["tmpfoo/x"]));
ok(!$fp_sib->[0]{pass}, "tmp/** must NOT admit tmpfoo/x");

my $fp_mixed = Eval::Assert::check(forbidden_sc(["tmp/**"]), forbidden_ev(["tmp/x", "other.txt"]));
is(scalar @$fp_mixed, 1, "mixed evidence emits one row per bad file and no clean row");
is($fp_mixed->[0]{name}, "forbidden_new_paths other.txt", "only the unallowed file is flagged");
ok(!$fp_mixed->[0]{pass}, "mixed evidence fails on the unallowed file");

my $fp_glob = Eval::Assert::check(forbidden_sc(["*.md"]), forbidden_ev(["notes.md"]));
my $fp_glob_fail = grep { !$_->{pass} } @$fp_glob;
ok(!$fp_glob_fail, "*.md admits notes.md at the root");
my $fp_glob2 = Eval::Assert::check(forbidden_sc(["*.md"]), forbidden_ev(["a/notes.md"]));
ok(!$fp_glob2->[0]{pass}, "*.md must NOT admit a/notes.md");

my $fp_fail_closed = Eval::Assert::check(forbidden_sc(["tmp/**"]), {
    transcript        => "",
    tool_calls        => [],
    git_commits       => 0,
    root              => $root,
    new_files         => undef,
    new_files_error   => "new-file walk failed: permission denied",
});
is(scalar @$fp_fail_closed, 1, "collection failure emits one row");
ok(!$fp_fail_closed->[0]{pass}, "forbidden_new_paths fails closed when evidence collection failed");
like($fp_fail_closed->[0]{detail}, qr/walk failed/, "failure detail carries the collection error");

my $fp_fail_closed2 = Eval::Assert::check(forbidden_sc(["tmp/**"]), {
    transcript  => "",
    tool_calls  => [],
    git_commits => 0,
    root        => $root,
});
ok(!$fp_fail_closed2->[0]{pass}, "forbidden_new_paths fails closed when new_files evidence is missing");
unlike($fp_fail_closed2->[0]{detail}, qr/clean/, "collection failure is never reported as clean");

done_testing
