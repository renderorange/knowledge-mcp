package Eval::Sandbox;

use strict;
use warnings;

# Perl resolves `use Eval::Sandbox` to Eval/Sandbox.pm under @INC. The
# implementation lives at evals/lib/Sandbox.pm per the plan's file layout;
# this loader bridges the package name to that file.
require 'Sandbox.pm';

1;
