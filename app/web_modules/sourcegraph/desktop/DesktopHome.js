// @flow

import React from "react";
import CSSModules from "react-css-modules";
import base from "sourcegraph/components/styles/_base.css";
import layout from "sourcegraph/components/styles/_layout.css";
import colors from "sourcegraph/components/styles/_colors.css";
import typography from "sourcegraph/components/styles/_typography.css";
import styles from "./styles/home.css";

import {Link} from "react-router";
import {Heading, List} from "sourcegraph/components";
import {Cone} from "sourcegraph/components/symbols";
import {inBeta} from "sourcegraph/user";
import * as betautil from "sourcegraph/util/betautil";

const NotInBeta = () => (
	<div className={`${layout.containerFixed} ${base.pv5} ${base.ph4}`} style={{maxWidth: "600px"}}>
	<Heading align="center" level="4" underline="blue">
		It looks like you're not in the desktop beta right now.
		Sign up to be eligible <a href="https://sourcegraph.com/beta">here</a>.
	</Heading>
	</div>
);

class DesktopHome extends React.Component {

	static contextTypes = {
		siteConfig: React.PropTypes.object.isRequired,
		user: React.PropTypes.object.isRequired,
	};

	render() {
		if (!this.context.user || !inBeta(this.context.user, betautil.DESKTOP)) {
			return <NotInBeta />;
		}
		return (
			<div className={`${layout.containerFixed} ${base.pv5} ${base.ph4}`} style={{maxWidth: "560px"}}>
				<Heading align="center" level="4" underline="blue">
					See live examples, search code, and view inline
					<br className={base["hidden-s"]} />&nbsp;documentation to write better code, faster
				</Heading>

				<img src={`${this.context.siteConfig.assetsRoot}/img/sg-desktop.gif`} width="356" title="Usage examples right in your editor" alt="Usage examples right in your editor" style={{maxWidth: "100%", display: "block", imageRendering: "pixelated"}} className={base.center}/>

				<div className={base.mv4}>
					<Heading level="5">Go definitions and usages as you code</Heading>
					<p>
						Install one of our editor integrations, and as you write Go code, this pane will update with contextually relevant information.
					</p>
				</div>
				<div className={base.mv4}>
					<Heading level="5">Semantic, global code search</Heading>
					<p>
						Just hit <span styleName="label-blue">⌘ or CTRL </span> + <span styleName="label-blue">SHIFT</span> + <span styleName="label-blue">;</span> or click the search box at the top of this page to semantically search for functions and symbols.
					</p>
				</div>
				<div className={base.mv4}>
					<Heading level="5">Powerful search for your private code</Heading>
					<p>
						To enable semantic search and usage examples for your private code, <Link to="/repositories">authorize Sourcegraph</Link> to access your private repositories.
					</p>
				</div>
				<div className={`${base.mt5} ${typography.f7}`}>
					<Heading level="6">
						<Cone width={16} className={`${colors["fill-orange"]} ${base.mr2}`} style={{
							verticalAlign: "baseline",
							position: "relative",
							top: "1px",
						}} />
						Sourcegraph Desktop is currently in beta
					</Heading>
					<p>
						Thanks for using Sourcegraph Desktop! If the app is not working as expected, see our GitHub to:
					</p>
					<List className={base.mv3}>
						<li><strong><a href="https://github.com/sourcegraph/sourcegraph-desktop#sourcegraph-desktop">Browse troubleshooting tips</a></strong></li>
						<li><strong><a href="https://github.com/sourcegraph/sourcegraph-desktop/issues/new">File an issue</a></strong></li>
					</List>
					<p>
						We love feedback! Shoot us an email at <strong><a href="mailto:support@sourcegraph.com?subject=Feedback for the Sourcegraph Desktop team&body=Editor of choice: %0D%0A%0D%0AOperating system:%0D%0A%0D%0AProgramming language:%0D%0A%0D%0AFeedback:">support@sourcegraph.com</a></strong> with ideas on how we can make Sourcegraph Desktop better.
					</p>
					<p>Did you know we use Slack for feedback and bugs? Let us know if you'd like to join our Slack channel!</p>
				</div>
			</div>
		);
	}
}

export default CSSModules(DesktopHome, styles, {allowMultiple: true});
