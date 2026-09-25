use strict;
use warnings;
use Test::More;
use FindBin;
use lib "$FindBin::Bin/../lib";
use Eval::Sandbox;

my $sc = {
    id      => 'pilot',
    prompt  => 'x',
    setup   => { files => [ { path => 'src/app.js', content => "console.log(1);\n" } ] },
    timeout_sec => 30,
};

my $sb = Eval::Sandbox->build($sc, { make_git => 1 });
ok( -d $sb->{home},    'home exists' );
ok( -d $sb->{project}, 'project exists' );
ok( -f "$sb->{project}/src/app.js", 'fixture written' );
ok( -f "$sb->{project}/AGENTS.md",  'AGENTS.md fixture copied' );
ok( -f "$sb->{home}/.config/opencode/opencode.jsonc", 'opencode config generated' );
ok( -d "$sb->{project}/.git", 'git initialized' );
ok( exists $sb->{manifest_before}{'src/app.js'}, 'manifest captured' );

my $cfg = do { local $/; open my $fh, '<', "$sb->{home}/.config/opencode/opencode.jsonc" or die $!; <$fh> };
like( $cfg, qr/knowledge-mcp/, 'config wires knowledge-mcp' );
like( $cfg, qr/\Q$sb->{project}\E/, 'config points at sandbox project' );
unlike( $cfg, qr/agents_global/, 'config never references real store' );

$sb->teardown();
ok( !-d $sb->{root}, 'teardown removes sandbox' );
done_testing();
