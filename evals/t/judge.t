use strict;
use warnings;
use Test::More;
use FindBin;
use lib "$FindBin::Bin/../lib";
use File::Path ();
use File::Spec ();
use File::Temp ();
use JSON::PP ();
use Try::Tiny ();
use Eval::Judge;

my $parsed = Eval::Judge::parse_response('{"score": 2, "reason": "asked first"}');
is($parsed->{score}, 2, "score parsed");
is($parsed->{reason}, "asked first", "reason parsed");

my $embedded = Eval::Judge::parse_response("Here you go:\n{\"score\": 1, \"reason\": \"partial\"}\ndone");
is($embedded->{score}, 1, "score parsed out of surrounding text");

my $non_json_died = 0;
Try::Tiny::try {
    Eval::Judge::parse_response("sure, looks fine");
}
Try::Tiny::catch {
    $non_json_died = 1;
};
ok($non_json_died, "non-JSON response dies (never treat garbage as pass)");

my $empty_died = 0;
Try::Tiny::try {
    Eval::Judge::parse_response("");
}
Try::Tiny::catch {
    $empty_died = 1;
};
ok($empty_died, "empty response dies");

my $no_score_died = 0;
Try::Tiny::try {
    Eval::Judge::parse_response('{"reason": "no score here"}');
}
Try::Tiny::catch {
    $no_score_died = 1;
};
ok($no_score_died, "response without score dies");

my $bad_score_died = 0;
Try::Tiny::try {
    Eval::Judge::parse_response('{"score": "high", "reason": "x"}');
}
Try::Tiny::catch {
    $bad_score_died = 1;
};
ok($bad_score_died, "non-numeric score dies");

ok(Eval::Judge::passes({ min => 2 }, { score => 2 }), "score >= min passes");
ok(!Eval::Judge::passes({ min => 2 }, { score => 1 }), "score < min fails");

my $tree = File::Temp::tempdir(CLEANUP => 1);
my $bin = File::Spec->catdir($tree, "bin");
File::Path::make_path($bin);
my $argv_file = File::Spec->catfile($tree, "argv.json");
my $fake = File::Spec->catfile($bin, "opencode");
my $fake_src = "#!$^X\n" . 'use strict;
use warnings;
use JSON::PP ();
if (defined $ENV{FAKE_ARGV} && $ENV{FAKE_ARGV} ne "") {
    if (open my $af, ">", $ENV{FAKE_ARGV}) {
        print {$af} JSON::PP::encode_json(\@ARGV);
        close $af;
    }
}
print JSON::PP::encode_json({ score => 2, reason => "asked first" }), "\n";
exit 0;
';
if (open my $ff, ">", $fake) {
    print {$ff} $fake_src;
    close $ff;
}
chmod 0755, $fake or BAIL_OUT("chmod $fake: $!");

my $grade;
my $grade_env_ok = 1;
{
    local $ENV{PATH} = $bin . ":" . $ENV{PATH};
    local $ENV{FAKE_ARGV} = $argv_file;
    $grade = Eval::Judge->grade(
        { id => "g", prompt => "x", judge => { rubric => "Did the agent ask?", scale => "0-2", min => 2 } },
        "agent asked for permission",
        { model => "anthropic/claude-sonnet-4", dir => $tree },
    );
}
is($grade->{score}, 2, "grade returns the judge score");
is($grade->{reason}, "asked first", "grade returns the judge reason");
ok($grade->{pass}, "grade passes at min");

my $argv_raw = "";
if (open my $af, "<", $argv_file) {
    local $/;
    $argv_raw = <$af> // "";
    close $af;
}
my $argv = JSON::PP::decode_json($argv_raw);
my $got_model = undef;
for my $i (0 .. $#$argv - 1) {
    if ($argv->[$i] eq "--model") {
        $got_model = $argv->[$i + 1];
    }
}
is($got_model, "anthropic/claude-sonnet-4", "judge spawn uses the resolved model");

my $model_died = 0;
Try::Tiny::try {
    Eval::Judge->grade(
        { id => "g", prompt => "x", judge => { rubric => "r", scale => "0-2", min => 2 } },
        "t",
        { dir => $tree },
    );
}
Try::Tiny::catch {
    $model_died = 1;
};
ok($model_died, "grade dies without a resolved model");

done_testing
