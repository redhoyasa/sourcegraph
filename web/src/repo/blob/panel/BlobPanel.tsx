import Loader from '@sourcegraph/icons/lib/Loader'
import RepoIcon from '@sourcegraph/icons/lib/Repo'
import { highlight } from 'highlight.js/lib/highlight'
import * as H from 'history'
import { castArray, isEqual } from 'lodash'
import marked from 'marked'
import * as React from 'react'
import { merge, Observable, of, Subject, Subscription } from 'rxjs'
import {
    bufferTime,
    catchError,
    concat,
    delay,
    distinctUntilChanged,
    map,
    scan,
    skip,
    startWith,
    switchMap,
    takeUntil,
} from 'rxjs/operators'
import { Location, MarkupContent, Position } from 'vscode-languageserver-types'
import { ServerCapabilities } from 'vscode-languageserver/lib/main'
import { AbsoluteRepoFile, PositionSpec } from '../..'
import {
    ExtensionsProps,
    getDefinition,
    getHover,
    getImplementations,
    getReferences,
    HoverMerged,
    ModeSpec,
} from '../../../backend/features'
import * as GQL from '../../../backend/graphqlschema'
import { fetchServerCapabilities, isEmptyHover, LSPTextDocumentPositionParams } from '../../../backend/lsp'
import { CXPControllerProps } from '../../../cxp/CXPEnvironment'
import { PanelItemPortal } from '../../../panel/PanelItemPortal'
import { PanelTitlePortal } from '../../../panel/PanelTitlePortal'
import { eventLogger } from '../../../tracking/eventLogger'
import { asError, ErrorLike, isErrorLike } from '../../../util/errors'
import { parseHash } from '../../../util/url'
import { RepoHeaderContributionsLifecycleProps } from '../../RepoHeader'
import { RepoHeaderContributionPortal } from '../../RepoHeaderContributionPortal'
import { RepoRevSidebarCommits } from '../../RepoRevSidebarCommits'
import { ToggleDiscussionsPanel } from '../actions/ToggleDiscussions'
import { ToggleHistoryPanel } from '../actions/ToggleHistoryPanel'
import { DiscussionsTree } from '../discussions/DiscussionsTree'
import { fetchExternalReferences } from '../references/backend'
import { FileLocations } from './FileLocations'
import { FileLocationsTree } from './FileLocationsTree'

interface Props
    extends AbsoluteRepoFile,
        Partial<PositionSpec>,
        ModeSpec,
        RepoHeaderContributionsLifecycleProps,
        ExtensionsProps,
        CXPControllerProps {
    location: H.Location
    history: H.History
    repoID: GQL.ID
    repoPath: string
    commitID: string
    isLightTheme: boolean
}

/** The subject (what the contextual information refers to). */
interface ContextSubject extends ModeSpec, ExtensionsProps {
    repoPath: string
    commitID: string
    filePath: string
    line: number
    character: number
}

export type BlobPanelTabID = 'info' | 'def' | 'references' | 'references:external' | 'discussions' | 'impl' | 'history'

function toSubject(props: Props): ContextSubject {
    const parsedHash = parseHash(props.location.hash)
    return {
        repoPath: props.repoPath,
        commitID: props.commitID,
        filePath: props.filePath,
        mode: props.mode,
        line: parsedHash.line || 1,
        character: parsedHash.character || 1,
        extensions: props.extensions,
    }
}

function subjectIsEqual(a: ContextSubject, b: ContextSubject & { line?: number; character?: number }): boolean {
    return (
        a &&
        b &&
        a.repoPath === b.repoPath &&
        a.commitID === b.commitID &&
        a.filePath === b.filePath &&
        a.mode === b.mode &&
        a.line === b.line &&
        a.character === b.character &&
        isEqual(a.extensions, b.extensions)
    )
}

const LOADING: 'loading' = 'loading'

interface State {
    /** The language server capabilities information. */
    serverCapabilitiesOrError?: ServerCapabilities | ErrorLike

    /** The hover information for the subject. */
    hoverOrError?: HoverMerged | ErrorLike | typeof LOADING
}

/**
 * A panel on the blob page that displays contextual information.
 */
export class BlobPanel extends React.PureComponent<Props, State> {
    public state: State = {}

    private componentUpdates = new Subject<Props>()
    private locationsUpdates = new Subject<void>()
    private subscriptions = new Subscription()

    public componentDidMount(): void {
        const componentUpdates = this.componentUpdates.pipe(startWith(this.props))

        // Changes to the context subject, including upon the initial mount.
        const subjectChanges = componentUpdates.pipe(
            distinctUntilChanged<Props>((a, b) => subjectIsEqual(toSubject(a), toSubject(b)))
        )

        // Update server capabilities.
        this.subscriptions.add(
            subjectChanges
                .pipe(
                    // This remains the same for all positions/ranges in the file.
                    distinctUntilChanged(
                        (a, b) =>
                            a.repoPath === b.repoPath &&
                            a.commitID === b.commitID &&
                            a.filePath === b.filePath &&
                            a.mode === b.mode
                    ),
                    switchMap(subject =>
                        fetchServerCapabilities({
                            repoPath: subject.repoPath,
                            rev: subject.rev,
                            commitID: subject.commitID,
                            filePath: subject.filePath,
                            mode: subject.mode,
                            settings: null,
                        }).pipe(
                            catchError(error => [asError(error)]),
                            map(c => ({ serverCapabilitiesOrError: c })),
                            startWith<Pick<State, 'serverCapabilitiesOrError'>>({
                                serverCapabilitiesOrError: undefined,
                            })
                        )
                    )
                )
                .subscribe(stateUpdate => this.setState(stateUpdate), error => console.error(error))
        )

        // Update hover.
        this.subscriptions.add(
            subjectChanges
                .pipe(
                    switchMap((subject: AbsoluteRepoFile & ModeSpec & ExtensionsProps & { position?: Position }) => {
                        const { position } = subject
                        if (
                            !position ||
                            position.character === 0 /* 1-indexed, so this means only line (not position) is selected */
                        ) {
                            return [{ hoverOrError: undefined }]
                        }
                        type PartialStateUpdate = Pick<State, 'hoverOrError'>
                        const result = getHover(
                            {
                                ...(subject as Pick<typeof subject, Exclude<keyof typeof subject, 'extensions'>>),
                                position,
                            },
                            { extensions: subject.extensions, cxpController: this.props.cxpController }
                        ).pipe(
                            catchError(error => [asError(error)]),
                            map(c => ({ hoverOrError: c } as PartialStateUpdate))
                        )
                        return merge(
                            result,
                            of({ hoverOrError: LOADING }).pipe(
                                delay(150),
                                takeUntil(result)
                            ) // delay loading spinner to reduce jitter
                        ).pipe(
                            startWith<PartialStateUpdate>({ hoverOrError: undefined }) // clear old data immediately)
                        )
                    })
                )
                .subscribe(stateUpdate => this.setState(stateUpdate), error => console.error(error))
        )

        // Update references when subject changes after the initial mount.
        this.subscriptions.add(subjectChanges.pipe(skip(1)).subscribe(() => this.locationsUpdates.next()))
    }

    public componentDidUpdate(): void {
        this.componentUpdates.next(this.props)
    }

    public componentWillUnmount(): void {
        this.subscriptions.unsubscribe()
    }

    public render(): JSX.Element | null {
        let titleRendered: React.ReactFragment | undefined
        let extraRendered: React.ReactFragment | undefined
        const { hoverOrError } = this.state
        if (hoverOrError === LOADING) {
            titleRendered = <Loader className="icon-inline" />
        } else if (hoverOrError === undefined) {
            // Don't show loading indicator yet (to reduce UI jitter).
            titleRendered = undefined
        } else if (hoverOrError && !isErrorLike(hoverOrError) && !isEmptyHover(hoverOrError)) {
            // Hover with one or more MarkedStrings.
            titleRendered = renderHoverContents(hoverOrError.contents[0])

            if (Array.isArray(hoverOrError.contents) && hoverOrError.contents.length >= 2) {
                extraRendered = hoverOrError.contents.slice(1).map((s, i) => (
                    <div key={i} className="blob-panel__extra-item px-2 pt-1">
                        {renderHoverContents(s)}
                    </div>
                ))
            }
        } else {
            // Error or no hover information.
            //
            // Don't bother showing the error, if any; if it occurs on the panel contents fetches, it will be
            // displayed.
        }

        const isValidToken = hoverOrError && hoverOrError !== LOADING && !isErrorLike(hoverOrError)

        const viewState = parseHash<BlobPanelTabID>(this.props.location.hash).viewState

        return (
            <>
                <RepoHeaderContributionPortal
                    position="right"
                    priority={20}
                    element={
                        <ToggleHistoryPanel
                            key="toggle-blob-panel"
                            location={this.props.location}
                            history={this.props.history}
                        />
                    }
                    repoHeaderContributionsLifecycleProps={this.props.repoHeaderContributionsLifecycleProps}
                />
                {window.context.discussionsEnabled && (
                    <RepoHeaderContributionPortal
                        position="right"
                        priority={20}
                        element={
                            <ToggleDiscussionsPanel
                                key="toggle-blob-discussion-panel"
                                location={this.props.location}
                                history={this.props.history}
                            />
                        }
                        repoHeaderContributionsLifecycleProps={this.props.repoHeaderContributionsLifecycleProps}
                    />
                )}

                {titleRendered && <PanelTitlePortal>{titleRendered}</PanelTitlePortal>}
                {extraRendered && (
                    <PanelItemPortal
                        id="info"
                        label="Info"
                        priority={1}
                        element={<div className="mt-2">{extraRendered}</div>}
                    />
                )}
                {(isValidToken || viewState === 'def') && (
                    <PanelItemPortal
                        id="def"
                        label="Definition"
                        priority={0}
                        element={
                            <FileLocations
                                className="panel__tabs-content"
                                query={this.queryDefinition}
                                updates={this.locationsUpdates}
                                inputRepo={this.props.repoPath}
                                inputRevision={this.props.rev}
                                // tslint:disable-next-line:jsx-no-lambda
                                onSelect={() => this.onSelectLocation('def')}
                                icon={RepoIcon}
                                pluralNoun="definitions"
                                isLightTheme={this.props.isLightTheme}
                            />
                        }
                    />
                )}
                {(isValidToken || viewState === 'references') && (
                    <PanelItemPortal
                        id="references"
                        label="References"
                        priority={-1}
                        element={
                            <FileLocations
                                className="panel__tabs-content"
                                query={this.queryReferencesLocal}
                                updates={this.locationsUpdates}
                                inputRepo={this.props.repoPath}
                                inputRevision={this.props.rev}
                                // tslint:disable-next-line:jsx-no-lambda
                                onSelect={() => this.onSelectLocation('references')}
                                icon={RepoIcon}
                                pluralNoun="local references"
                                isLightTheme={this.props.isLightTheme}
                            />
                        }
                    />
                )}
                {(isValidToken || viewState === 'references:external') && (
                    <PanelItemPortal
                        id="references:external"
                        label="External references"
                        priority={-2}
                        element={
                            <FileLocationsTree
                                className="panel__tabs-content"
                                query={this.queryReferencesExternal}
                                updates={this.locationsUpdates}
                                // tslint:disable-next-line:jsx-no-lambda
                                onSelectLocation={() => this.onSelectLocation('references:external')}
                                icon={RepoIcon}
                                pluralNoun="external references"
                                isLightTheme={this.props.isLightTheme}
                                location={this.props.location}
                            />
                        }
                    />
                )}
                {(isValidToken || viewState === 'impl') && (
                    <PanelItemPortal
                        id="impl"
                        label="Implementation"
                        priority={-3}
                        hidden={
                            !this.state.serverCapabilitiesOrError ||
                            isErrorLike(this.state.serverCapabilitiesOrError) ||
                            !this.state.serverCapabilitiesOrError.implementationProvider
                        }
                        element={
                            <FileLocations
                                className="panel__tabs-content"
                                query={this.queryImplementation}
                                updates={this.locationsUpdates}
                                inputRepo={this.props.repoPath}
                                inputRevision={this.props.rev}
                                // tslint:disable-next-line:jsx-no-lambda
                                onSelect={() => this.onSelectLocation('impl')}
                                icon={RepoIcon}
                                pluralNoun="implementations"
                                isLightTheme={this.props.isLightTheme}
                            />
                        }
                    />
                )}
                <PanelItemPortal
                    id="history"
                    label="File history"
                    priority={-4}
                    element={
                        <RepoRevSidebarCommits
                            key="commits"
                            repoID={this.props.repoID}
                            rev={this.props.rev}
                            filePath={this.props.filePath}
                            history={this.props.history}
                            location={this.props.location}
                        />
                    }
                />
                {window.context.discussionsEnabled && (
                    <PanelItemPortal
                        id="discussions"
                        label="File discussions"
                        priority={-5}
                        element={
                            <DiscussionsTree
                                repoID={this.props.repoID}
                                repoPath={this.props.repoPath}
                                commitID={this.props.commitID}
                                rev={this.props.rev}
                                filePath={this.props.filePath}
                                history={this.props.history}
                                location={this.props.location}
                            />
                        }
                    />
                )}
            </>
        )
    }

    private onSelectLocation = (tab: BlobPanelTabID): void => eventLogger.log('BlobPanelLocationSelected', { tab })

    private queryDefinition = (): Observable<{ loading: boolean; locations: Location[] }> =>
        getDefinition(this.props as LSPTextDocumentPositionParams, this.props).pipe(
            map(locations => ({ loading: false, locations: locations ? castArray(locations) : [] }))
        )

    private queryReferencesLocal = (): Observable<{ loading: boolean; locations: Location[] }> =>
        getReferences({ ...(this.props as LSPTextDocumentPositionParams), includeDeclaration: false }, this.props).pipe(
            map(c => ({ loading: false, locations: c }))
        )

    private queryReferencesExternal = (): Observable<{ loading: boolean; locations: Location[] }> =>
        fetchExternalReferences(this.props as LSPTextDocumentPositionParams, this.props.extensions).pipe(
            map(c => ({ loading: true, locations: c })),
            concat([{ loading: false, locations: [] }]),
            bufferTime(500), // reduce UI jitter
            scan<{ loading: boolean; locations: Location[] }[], { loading: boolean; locations: Location[] }>(
                (cur, locs) => ({
                    loading: cur.loading && locs.every(({ loading }) => loading),
                    locations: cur.locations.concat(...locs.map(({ locations }) => locations)),
                }),
                { loading: true, locations: [] }
            )
        )

    private queryImplementation = (): Observable<{ loading: boolean; locations: Location[] }> =>
        getImplementations(this.props as LSPTextDocumentPositionParams, this.props).pipe(
            map(c => ({ loading: false, locations: c }))
        )
}

function renderHoverContents(contents: HoverMerged['contents'][0]): React.ReactFragment {
    const value = typeof contents === 'string' ? contents : contents.value
    const language =
        typeof contents === 'string' ? 'markdown' : MarkupContent.is(contents) ? contents.kind : contents.language
    try {
        if (language === 'markdown') {
            return (
                <div
                    dangerouslySetInnerHTML={{
                        __html: marked(value, { gfm: true, breaks: true, sanitize: true }),
                    }}
                />
            )
        }
        return <code className="hljs" dangerouslySetInnerHTML={{ __html: highlight(language, value).value }} />
    } catch (e) {
        // Ignore rendering or syntax highlighting errors; plain text will be rendered.
    }
    return value
}
