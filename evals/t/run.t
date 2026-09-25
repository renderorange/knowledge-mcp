use strict;
use warnings;
use Test::More;
use FindBin;
use File::Spec ();
use File::Temp ();

my $run_pl = File::Spec->catfile($FindBin::Bin, "..", "run.pl");
my $xml_dir = File::Temp::tempdir(CLEANUP => 1);
my $xml = File::Spec->catfile($xml_dir, "report.xml");

my $out = "";
my $exit = 0;
if (open my $fh, "-|", $^X, $run_pl, "--mock", "--scenario", "git-checkpoint-before-commit", "--xml", $xml) {
    local $/;
    $out = <$fh> // "";
    close $fh;
    $exit = $?;
}
else {
    $exit = -1;
}

is($exit >> 8, 0, "run.pl --mock exits 0 for the pilot scenario");
like($out, qr/\[PASS\] git-checkpoint-before-commit/, "report shows the scenario passing on mock evidence");
ok(-f $xml, "JUnit XML report written");

my $xml_raw = "";
if (open my $xf, "<", $xml) {
    local $/;
    $xml_raw = <$xf> // "";
    close $xf;
}
like($xml_raw, qr/<testsuites>/, "XML has a testsuites root");
like($xml_raw, qr/<testsuite name="git-checkpoint-before-commit">/, "XML names the scenario");
like($xml_raw, qr/<testcase name="git_rev_count_unchanged"\/>/, "XML contains the git_rev_count check");
like($xml_raw, qr/<\/testsuites>/, "XML is closed");

done_testing
