n=int(input()); a=list(map(int,input().split()))
def solve(a):
    if len(a)<=1: return a,0
    m=len(a)//2; l,x=solve(a[:m]); r,y=solve(a[m:]); i=j=0; out=[]; ans=x+y
    while i<len(l) and j<len(r):
        if l[i]<=r[j]: out.append(l[i]); i+=1
        else: out.append(r[j]); j+=1; ans+=len(l)-i
    return out+l[i:]+r[j:],ans
print(solve(a)[1])
